import os
import json
import urllib.request
import urllib.error
import urllib.parse
import sys

BASE = os.environ.get("KEEL_BASE", "http://127.0.0.1:9000").rstrip("/")
PASS = []
FAIL = []


def req(method, path, data=None, token=None, raw=False):
    body = json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(BASE + path, data=body, method=method)
    if body is not None:
        r.add_header("Content-Type", "application/json")
    if token:
        r.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            b = resp.read()
            return resp.status, (b if raw else json.loads(b.decode() or "{}"))
    except urllib.error.HTTPError as e:
        b = e.read()
        try:
            return e.code, json.loads(b.decode() or "{}")
        except Exception:
            return e.code, b.decode(errors="replace")


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(("  PASS " if cond else "  FAIL ") + name + ((" | " + str(detail)[:160]) if detail else ""))


st, r = req("POST", "/api/v1/auth/login", {
    "username": "admin", "password": os.environ.get("KEEL_ADMIN_PASS", "Admin@123456"),
    "captchaId": "", "captchaCode": "", "totpCode": "",
})
token = r["data"]["token"]["accessToken"]
check("管理员登录", st == 200 and bool(token))

print("\n--- 备份恢复 ---")
st, r = req("POST", "/api/v1/maintenance/backups", {}, token=token)
check("立即备份", st == 200 and r.get("data", {}).get("name", "").startswith("keel-"), r.get("data"))
name = r.get("data", {}).get("name", "")
meta = r.get("data", {})
check("备份元信息含校验和", bool(meta.get("sha256")) and len(meta.get("sha256", "")) == 64)
check("备份为 gzip 压缩", meta.get("compressed") is True)
check("备份产物为 tar 容器", meta.get("name", "").endswith(".tar.gz"))
check("备份记录表清单", isinstance(meta.get("tables"), list) and "sys_user" in meta.get("tables", []))
check("元信息含文件资产字段", "files" in meta and "storageMode" in meta,
      f"files={meta.get('files')} storageMode={meta.get('storageMode')}")

st, r = req("GET", "/api/v1/maintenance/backups", token=token)
files = r.get("data", [])
check("备份列表包含新文件", st == 200 and any(f["name"] == name for f in files), f"共 {len(files)} 份")

q = urllib.parse.quote(name, safe="")
st, r = req("POST", f"/api/v1/maintenance/backups/{q}/verify", {}, token=token)
v = r.get("data", {})
check("校验通过", st == 200 and v.get("ok") is True, v.get("integrity"))
check("校验和比对一致", v.get("metaMatched") is True)
check("校验读回用户数", isinstance(v.get("userCount"), int) and v.get("userCount", 0) > 0, v.get("userCount"))
check("校验含文件资产计数", isinstance(v.get("files"), int), v.get("files"))

st, body = req("GET", f"/api/v1/maintenance/backups/{q}/download", token=token, raw=True)
check("下载备份文件", st == 200 and len(body) > 100, f"{len(body)} bytes")
check("下载为 gzip 魔数", body[:2] == b"\x1f\x8b")

st, r = req("POST", f"/api/v1/maintenance/backups/{q}/restore?dry_run=true", {}, token=token)
plan = r.get("data", {})
check("恢复演练返回计划", st == 200 and plan.get("dryRun") is True and len(plan.get("plan", [])) > 0,
      plan.get("plan"))
check("演练标注停机模式", plan.get("mode") == "offline", plan.get("mode"))

st, r = req("GET", "/api/v1/maintenance/prune-plan", token=token)
pp = r.get("data", {})
check("GFS 保留演练可查询", st == 200 and isinstance(pp.get("keep"), list) and "slots" in pp, pp.get("slots"))

st, r = req("POST", f"/api/v1/maintenance/backups/{q}/restore", {}, token=token)
check("未开启 allow_restore 时拒绝在线恢复", st == 400, r.get("message"))

st, r = req("GET", "/api/v1/jobs?page_size=100", token=token)
jobs = r.get("data", {}).get("list", [])
backup_job = next((j for j in jobs if j.get("handler") == "db.backup"), None)
check("内置备份任务已注册", backup_job is not None, backup_job and backup_job.get("name"))
if backup_job:
    st, r = req("POST", f"/api/v1/jobs/{backup_job['id']}/run", {}, token=token)
    check("手动执行备份任务", st == 200, r.get("data", {}).get("message") if isinstance(r.get("data"), dict) else r.get("message"))

st, r = req("DELETE", f"/api/v1/maintenance/backups/{q}", token=token)
check("删除备份", st == 200)
st, r = req("GET", "/api/v1/maintenance/backups", token=token)
check("删除后列表不再包含", all(f["name"] != name for f in r.get("data", [])))

print("\n--- 版本化迁移 ---")
st, r = req("GET", "/api/v1/maintenance/migrations", token=token)
m = r.get("data", {})
check("迁移状态可查询", st == 200 and m.get("enabled") is True)
status = m.get("status", {})
check("当前版本=最新版本", status.get("current") == status.get("latest") and status.get("pending") == 0, status)
check("版本清单含 baseline/oauth/回填/索引修复", [v["name"] for v in status.get("versions", [])][:4] ==
      ["baseline", "oauth_bindings", "backfill_user_nickname", "fix_oauth_binding_unique_indexes"])
st, r = req("POST", "/api/v1/maintenance/migrations/up", {}, token=token)
check("重复升级幂等（0 个应用）", st == 200 and len(r.get("data", {}).get("applied", [])) == 0, r.get("data"))

print("\n--- 自动接口文档 ---")
st, body = req("GET", "/docs/doc.json", raw=True)
doc = json.loads(body)
check("OpenAPI 描述可访问", st == 200 and doc.get("swagger") == "2.0")
check("文档覆盖 100+ 路径", len(doc.get("paths", {})) >= 100, len(doc.get("paths", {})))
check("文档含 OAuth 接口", "/api/v1/oauth/providers" in doc.get("paths", {}))
check("文档含备份接口", "/api/v1/maintenance/backups" in doc.get("paths", {}))
check("文档含开放接口", any(p.startswith("/api/open/") for p in doc.get("paths", {})))
check("文档含两种鉴权定义", set(doc.get("securityDefinitions", {}).keys()) == {"BearerAuth", "ApiKeyAuth"})
st, _ = req("GET", "/docs/index.html", raw=True)
check("Swagger UI 可访问", st == 200)

print("\n--- OAuth 配置（未启用时）---")
st, r = req("GET", "/api/v1/oauth/providers")
check("渠道列表公开可访问", st == 200 and r.get("data", {}).get("enabled") is False)
check("未配置时不下发渠道", r.get("data", {}).get("providers") == [])

print(f"\n结果：{len(PASS)} 通过 / {len(FAIL)} 失败")
if FAIL:
    print("失败项：", FAIL)
    sys.exit(1)
