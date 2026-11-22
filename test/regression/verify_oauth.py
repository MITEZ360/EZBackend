import os
import json
import sys
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get("KEEL_BASE", "http://127.0.0.1:9002").rstrip("/")
PASS, FAIL = [], []


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(("  PASS " if cond else "  FAIL ") + name + ((" | " + str(detail)[:180]) if detail else ""))


def req(method, path, data=None, token=None):
    body = json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(BASE + path, data=body, method=method)
    if body is not None:
        r.add_header("Content-Type", "application/json")
    if token:
        r.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(r, timeout=20) as resp:
            return resp.status, json.loads(resp.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode() or "{}")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


opener = urllib.request.build_opener(NoRedirect)


def location(url):
    if url.startswith("/"):
        url = BASE + url
    try:
        with opener.open(urllib.request.Request(url), timeout=15) as r:
            return r.headers.get("Location")
    except urllib.error.HTTPError as e:
        return e.headers.get("Location")


def login(provider):
    st, r = req("GET", f"/api/v1/oauth/authorize/{provider}")
    assert st == 200, r
    auth_url = r["data"]["url"]
    cb = location(auth_url)
    assert cb and "code=" in cb and "state=" in cb, cb
    final = location(cb)
    assert final and "#" in final, final
    frag = urllib.parse.parse_qs(urllib.parse.urlparse(final).fragment)
    assert frag.get("access_token"), final[:200]
    return frag["access_token"][0], cb, frag


def admin_token():
    st, r = req("POST", "/api/v1/auth/login", {
        "username": "admin", "password": os.environ.get("KEEL_ADMIN_PASS", "Admin@123456"),
        "captchaId": "", "captchaCode": "", "totpCode": "",
    })
    return r["data"]["token"]["accessToken"]


def user_total(token):
    st, r = req("GET", "/api/v1/users?page=1&page_size=1", token=token)
    return r.get("data", {}).get("total", -1)


print("--- 配置下发 ---")
st, r = req("GET", "/api/v1/oauth/providers")
check("渠道列表公开且含 2 个配置渠道", st == 200 and len(r["data"]["providers"]) == 2,
      [p["name"] for p in r["data"]["providers"]])
check("渠道不下发凭据", all("client" not in p for p in r["data"]["providers"]))

adm = admin_token()
before = user_total(adm)
check("基线用户数可读", before > 0, before)

print("\n--- provider 1：默认字段映射（OIDC 风格）---")
tok1, cb1, frag1 = login("demo-oidc")
st, p1 = req("GET", "/api/v1/auth/profile", token=tok1)
check("OIDC 登录成功并签发本地令牌", st == 200 and p1["data"]["user"]["username"] == "bob",
      p1["data"]["user"]["username"])
check("字段映射取到邮箱", p1["data"]["user"].get("email") == "bob@example.com")
check("自动注册用户非超管", p1["data"]["user"].get("isSuper") is False)
check("fragment 携带 refresh_token", bool(frag1.get("refresh_token")))

st, b1 = req("GET", "/api/v1/oauth/bindings", token=tok1)
check("绑定记录已建立", st == 200 and len(b1["data"]) == 1 and b1["data"][0]["provider"] == "demo-oidc",
      b1["data"])

tok1b, _, _ = login("demo-oidc")
st, p1b = req("GET", "/api/v1/auth/profile", token=tok1b)
check("二次登录命中绑定，不重复建号", p1b["data"]["user"]["id"] == p1["data"]["user"]["id"])

print("\n--- provider 2：自定义 field_map + 独立邮箱接口 ---")
tok2, _, _ = login("demo-github")
st, p2 = req("GET", "/api/v1/auth/profile", token=tok2)
check("GitHub 风格登录成功", st == 200 and p2["data"]["user"]["username"] == "carol",
      p2["data"]["user"]["username"])
check("email_url 补齐邮箱", p2["data"]["user"].get("email") == "carol@example.com", p2["data"]["user"].get("email"))
st, b2 = req("GET", "/api/v1/oauth/bindings", token=tok2)
check("GitHub 渠道绑定独立", st == 200 and b2["data"][0]["provider"] == "demo-github")

after = user_total(adm)
check("两个渠道共新增 2 个用户（重复登录不增）", after - before == 2, f"{before} -> {after}")

print("\n--- 安全边界 ---")
final_again = location(cb1)
check("state 一次性消费，重放被拒", final_again and "oauth_error" in final_again, final_again)

st, r = req("GET", "/api/v1/oauth/authorize/not-exist")
check("未配置渠道返回 404", st == 404, r.get("message"))

st, r = req("DELETE", "/api/v1/oauth/bindings/demo-github", token=tok2)
check("解绑成功", st == 200, r.get("message"))
st, b2 = req("GET", "/api/v1/oauth/bindings", token=tok2)
check("解绑后绑定记录清空", st == 200 and len(b2["data"]) == 0)

tok3, _, _ = login("demo-github")
st, p3 = req("GET", "/api/v1/auth/profile", token=tok3)
check("解绑后重新登录会新建账号（auto_register）",
      p3["data"]["user"]["id"] != p2["data"]["user"]["id"], p3["data"]["user"]["username"])

print(f"\n结果：{len(PASS)} 通过 / {len(FAIL)} 失败")
if FAIL:
    print("失败项：", FAIL)
    sys.exit(1)
