import os
import json
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get("KEEL_BASE", "http://127.0.0.1:9000").rstrip("/")
ok = 0
fail = 0


def call(method, path, body=None, token=None, apikey=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if apikey:
        req.add_header("X-API-Key", apikey)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw or "{}")
        except Exception:
            return e.code, {"raw": raw}


def check(name, cond, detail=""):
    global ok, fail
    if cond:
        ok += 1
        print(f"  PASS  {name}")
    else:
        fail += 1
        print(f"  FAIL  {name}  {detail}")


stamp = str(int(time.time()))[-6:]
suffix = stamp
code_cat = "demo-" + suffix
code_src = "src-" + suffix

print("== 登录 ==")
st, r = call("POST", "/api/v1/auth/login", {
    "username": "admin", "password": os.environ.get("KEEL_ADMIN_PASS", "Admin@123456"),
    "captchaId": "", "captchaCode": "", "totpCode": "",
})
tk = (r.get("data") or {}).get("token") or {}
token = tk.get("accessToken") if isinstance(tk, dict) else tk
check("admin 登录", st == 200 and token, f"{st} {r}")

print("== 分类配置 ==")
st, r = call("GET", "/api/v1/news-categories", token=token)
cats = r.get("data") or []
check("内置分类已下发", st == 200 and len(cats) >= 7, f"{st} {len(cats)}")
check("tech 分类存在", any(c["code"] == "tech" for c in cats))

st, r = call("POST", "/api/v1/news-categories", {
    "code": code_cat, "name": "演示分类" + suffix, "icon": "/uploads/demo.png",
    "color": "#123456", "sort": 88, "remark": "回归测试",
}, token)
cat = r.get("data") or {}
check("新增分类", st == 200 and cat.get("code") == code_cat, f"{st} {r}")
cat_id = cat.get("id")

st, r = call("POST", "/api/v1/news-categories", {
    "code": code_cat, "name": "重复",
}, token)
check("重复编码 409", st == 409, f"{st} {r}")

st, r = call("GET", "/api/v1/news-categories", token=token)
mine = [c for c in (r.get("data") or []) if c["code"] == code_cat]
check("分类回读带图标与颜色",
      len(mine) == 1 and mine[0]["icon"] == "/uploads/demo.png" and mine[0]["color"] == "#123456",
      str(mine))

print("== 来源配置 ==")
st, r = call("POST", "/api/v1/news-sources", {
    "code": code_src, "name": "演示来源" + suffix, "kind": "crawler",
    "icon": "/uploads/src.png", "color": "#0F766E", "sort": 66,
}, token)
src = r.get("data") or {}
check("新增来源", st == 200 and src.get("code") == code_src, f"{st} {r}")
src_id = src.get("id")

print("== 开放端元数据 ==")
st, _ = call("GET", "/api/open/v1/news/meta")
check("无密钥访问 401", st == 401, str(st))

st, r = call("POST", "/api/v1/api-keys", {
    "name": "回归密钥" + suffix, "kind": "service",
    "scopes": ["news:read", "news:ingest"], "rateRps": 0, "dailyQuota": 0,
}, token)
issued = r.get("data") or {}
plain = issued.get("plain")
key_id = (issued.get("key") or {}).get("id")
check("签发 API Key", st == 200 and plain, f"{st} {r}")

st, r = call("GET", "/api/open/v1/news/meta", apikey=plain)
meta = r.get("data") or {}
msrc = [s for s in meta.get("sources", []) if s["code"] == code_src]
mcat = [c for c in meta.get("categories", []) if c["code"] == code_cat]
check("meta 返回来源及图标",
      len(msrc) == 1 and msrc[0]["icon"] == "/uploads/src.png" and msrc[0]["color"] == "#0F766E",
      str(msrc))
check("meta 返回分类及图标",
      len(mcat) == 1 and mcat[0]["icon"] == "/uploads/demo.png" and mcat[0]["color"] == "#123456",
      str(mcat))
check("meta 保留天数 7", meta.get("retentionDays") == 7, str(meta.get("retentionDays")))
check("meta 分类按排序返回",
      [c["sort"] for c in meta.get("categories", [])] == sorted(c["sort"] for c in meta.get("categories", [])))

print("== 白名单校验 ==")
now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
st, r = call("POST", "/api/open/v1/news/batch", {
    "source": "ghost-" + suffix,
    "items": [{"title": "未知来源", "category": code_cat, "publishedAt": now}],
}, apikey=plain)
check("未知来源整批拒绝", st == 403, f"{st} {r}")

st, r = call("GET", "/api/v1/news-sources?keyword=ghost-" + suffix, token=token)
check("未知来源未被自动注册", (r.get("data") or {}).get("total", 0) == 0, str(r.get("data")))

st, r = call("POST", "/api/open/v1/news/batch", {
    "source": code_src,
    "items": [
        {"title": "坏分类" + suffix, "category": "nope-" + suffix, "publishedAt": now},
        {"title": "好分类" + suffix, "category": code_cat, "publishedAt": now},
    ],
}, apikey=plain)
res = r.get("data") or {}
check("未知分类单条拒收", st == 200 and res.get("accepted") == 1 and res.get("invalid") == 1, f"{st} {r}")
check("错误明细带下标与原因",
      (res.get("errors") or [{}])[0].get("index") == 0 and "分类" in (res.get("errors") or [{}])[0].get("reason", ""),
      str(res.get("errors")))

st, r = call("POST", "/api/open/v1/news/batch", {
    "source": code_src,
    "items": [{"title": "无分类" + suffix, "publishedAt": now}],
}, apikey=plain)
res = r.get("data") or {}
check("缺失分类不再兜底 general", st == 200 and res.get("accepted", 0) == 0 and res.get("invalid") == 1, f"{st} {r}")

print("== 停用分类后拒收 ==")
st, r = call("PUT", f"/api/v1/news-categories/{cat_id}", {
    "code": code_cat, "name": "演示分类" + suffix, "sort": 88, "status": 0,
}, token)
check("停用分类", st == 200 and (r.get("data") or {}).get("status") == 0, f"{st} {r}")

st, r = call("POST", "/api/open/v1/news/batch", {
    "source": code_src,
    "items": [{"title": "停用后" + suffix, "category": code_cat, "publishedAt": now}],
}, apikey=plain)
res = r.get("data") or {}
check("停用分类被拒收", st == 200 and res.get("accepted", 0) == 0 and res.get("invalid") == 1, f"{st} {r}")

st, r = call("GET", "/api/open/v1/news/meta", apikey=plain)
check("停用分类不下发", all(c["code"] != code_cat for c in (r.get("data") or {}).get("categories", [])))

print("== 删除保护 ==")
st, r = call("DELETE", f"/api/v1/news-categories/{cat_id}", token=token)
check("有资讯引用的分类拒绝删除", st == 409, f"{st} {r}")

print("== 清理回收 ==")
st, r = call("GET", f"/api/v1/news?sourceId={src_id}&page_size=50", token=token)
for row in (r.get("data") or {}).get("list", []):
    call("DELETE", "/api/v1/news/" + row["id"], token=token)
call("DELETE", f"/api/v1/news-categories/{cat_id}", token=token)
call("DELETE", f"/api/v1/news-sources/{src_id}", token=token)
if key_id:
    call("DELETE", f"/api/v1/api-keys/{key_id}", token=token)

st, r = call("GET", "/api/v1/news-categories", token=token)
check("测试数据已回收", all(c["code"] != code_cat for c in (r.get("data") or [])))

print(f"\n结果：{ok} 通过 / {fail} 失败")
raise SystemExit(0 if fail == 0 else 1)
