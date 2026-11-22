import os
import json
import urllib.request
import urllib.error

BASE = os.environ.get("KEEL_BASE", "http://127.0.0.1:9102").rstrip("/")
ok_count = 0
fail_count = 0


def call(method, path, token=None, body=None, key=None, base=BASE):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if key:
        req.add_header("X-API-Key", key)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            raw = r.read().decode()
            return r.status, (json.loads(raw) if raw else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


def check(name, cond, extra=""):
    global ok_count, fail_count
    if cond:
        ok_count += 1
        print(f"  PASS  {name}")
    else:
        fail_count += 1
        print(f"  FAIL  {name}  {extra}")


def login(u, p):
    st, r = call("POST", "/api/v1/auth/login", body={"username": u, "password": p})
    assert st == 200, r
    return r["data"]["token"]["accessToken"]


print("== 登录与列表 ==")
admin = login("admin", os.environ.get("KEEL_ADMIN_PASS", "Admin@123456"))
st, r = call("GET", "/api/v1/api-keys", admin)
check("初始列表为空", st == 200 and r["data"]["total"] == 0, r)

print("== 签发服务端密钥 ==")
st, r = call("POST", "/api/v1/api-keys", admin, {
    "name": "资讯爬虫-生产",
    "kind": "service",
    "scopes": ["news:ingest"],
    "ipAllow": ["127.0.0.1"],
    "rateRps": 50,
    "dailyQuota": 1000,
    "remark": "生产爬虫",
})
check("签发成功", st == 200 and r["data"]["plain"].startswith("ak_s_"), r)
svc_key = r["data"]["plain"]
svc_id = r["data"]["key"]["id"]
check("明文长度 45", len(svc_key) == 45, len(svc_key))
check("前缀与明文匹配", r["data"]["key"]["prefix"] == svc_key[:12], r["data"]["key"]["prefix"])
check("响应不含哈希", "secretHash" not in json.dumps(r["data"]["key"]))

st, r = call("GET", "/api/v1/api-keys", admin)
row = r["data"]["list"][0]
check("列表不含完整密钥与哈希", svc_key not in json.dumps(r) and "secretHash" not in json.dumps(r))
check("列表带用量字段", "todayUsage" in row)

print("== 开放接口认证 ==")
st, r = call("GET", "/api/open/v1/whoami", key=svc_key)
check("密钥可调用开放接口", st == 200 and r["data"]["kind"] == "service", r)
check("返回 scope", r["data"]["scopes"] == ["news:ingest"], r["data"].get("scopes"))
check("返回配额", r["data"]["dailyQuota"] == 1000 and r["data"]["todayUsage"] >= 1, r["data"])

st, r = call("GET", "/api/open/v1/whoami")
check("缺少密钥返回 40107", st == 401 and r["code"] == 40107, r)
st, r = call("GET", "/api/open/v1/whoami", key="ak_s_fake")
check("错误密钥返回 40107", st == 401 and r["code"] == 40107, r)

print("== 客户端密钥与 IP 白名单 ==")
st, r = call("POST", "/api/v1/api-keys", admin, {
    "name": "移动端", "kind": "client", "scopes": ["news:read"], "dailyQuota": 500,
})
client_key = r["data"]["plain"]
check("客户端密钥前缀 ak_c_", client_key.startswith("ak_c_"), client_key)
st, r = call("GET", "/api/open/v1/whoami", key=client_key)
check("客户端密钥可用", st == 200 and r["data"]["kind"] == "client", r)

st, r = call("POST", "/api/v1/api-keys", admin, {
    "name": "仅内网", "kind": "service", "scopes": ["news:read"], "ipAllow": ["10.0.0.0/8"],
})
lan_key = r["data"]["plain"]
st, r = call("GET", "/api/open/v1/whoami", key=lan_key)
check("白名单外 IP 被拒", st == 403, r)

print("== 停用与轮换 ==")
st, _ = call("PATCH", f"/api/v1/api-keys/{svc_id}/status", admin, {"status": 0})
st, r = call("GET", "/api/open/v1/whoami", key=svc_key)
check("停用后拒绝", st == 401 and r["code"] == 40107, r)

call("PATCH", f"/api/v1/api-keys/{svc_id}/status", admin, {"status": 1})
st, r = call("POST", f"/api/v1/api-keys/{svc_id}/rotate", admin)
new_key = r["data"]["plain"]
check("轮换生成新密钥", st == 200 and new_key != svc_key, r)
st, r = call("GET", "/api/open/v1/whoami", key=svc_key)
check("旧密钥失效", st == 401, r)
st, r = call("GET", "/api/open/v1/whoami", key=new_key)
check("新密钥可用", st == 200, r)

print("== 每日配额 ==")
st, r = call("POST", "/api/v1/api-keys", admin, {
    "name": "低配额", "kind": "service", "scopes": ["news:read"], "dailyQuota": 2,
})
q_key = r["data"]["plain"]
codes = []
for _ in range(3):
    st, r = call("GET", "/api/open/v1/whoami", key=q_key)
    codes.append(st)
check("配额用尽返回 429", codes == [200, 200, 429], codes)

print("== 权限边界 ==")
demo = login("demo", "Demo@123456")
st, r = call("GET", "/api/v1/api-keys", demo)
check("普通用户不能查看密钥", st == 403, r)
st, r = call("POST", "/api/v1/api-keys", demo, {"name": "x", "scopes": ["news:read"]})
check("普通用户不能签发密钥", st == 403, r)

print("== 删除 ==")
st, _ = call("DELETE", f"/api/v1/api-keys/{svc_id}", admin)
st, r = call("GET", "/api/open/v1/whoami", key=new_key)
check("删除后失效", st == 401, r)

print(f"\n结果：{ok_count} 通过，{fail_count} 失败")
