import os
import json
import sqlite3
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from urllib.parse import quote

BASE = os.environ.get("TENON_BASE", "http://127.0.0.1:9103").rstrip("/")
ok_count = 0
fail_count = 0


def call(method, path, token=None, body=None, key=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if key:
        req.add_header("X-API-Key", key)
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
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


now = datetime.now(timezone.utc)

print("== 准备数据源与分类（白名单，需先配置）==")
admin = login("admin", os.environ.get("TENON_ADMIN_PASS", "Admin@123456"))
# 幂等：已存在则跳过（409/重复）
st, r = call("GET", "/api/v1/news-sources", admin)
existing_sources = {s["code"] for s in (r.get("data", {}).get("list", []) if isinstance(r.get("data"), dict) else [])}
if "tech-daily" not in existing_sources:
    st, r = call("POST", "/api/v1/news-sources", admin, body={
        "code": "tech-daily", "name": "科技日报", "kind": "crawler", "status": 1,
    })
    check("创建数据源 tech-daily", st == 200, r.get("message"))
st, r = call("GET", "/api/v1/news-categories", admin)
existing_cats = {c["code"] for c in (r.get("data", []) if isinstance(r.get("data"), list) else [])}
for code, name in [("tech", "科技"), ("finance", "财经")]:
    if code not in existing_cats:
        st, r = call("POST", "/api/v1/news-categories", admin, body={
            "code": code, "name": name, "status": 1,
        })
        check(f"创建分类 {code}", st == 200, r.get("message"))

print("== 准备密钥 ==")
st, r = call("POST", "/api/v1/api-keys", admin, {
    "name": "资讯爬虫", "kind": "service",
    "scopes": ["news:ingest", "news:read"], "dailyQuota": 100000,
})
ingest_key = r["data"]["plain"]
st, r = call("POST", "/api/v1/api-keys", admin, {
    "name": "只读客户端", "kind": "client", "scopes": ["news:read"],
})
read_key = r["data"]["plain"]
check("签发爬虫密钥", st == 200 and ingest_key.startswith("ak_s_"), r)

print("== 批量导入 ==")
items = []
for i in range(20):
    items.append({
        "externalId": f"ext-{i}",
        "title": f"测试资讯-{i:02d}",
        "summary": f"第 {i} 条测试资讯的摘要",
        "content": f"这是第 {i} 条测试资讯的正文内容，用于验证存储与检索。" * 3,
        "url": f"https://example.com/news/{i}",
        "category": "tech" if i % 2 == 0 else "finance",
        "tags": ["测试", f"标签{i % 3}"],
        "publishedAt": (now - timedelta(minutes=i * 10)).isoformat(),
    })
items.append({"title": "", "content": "无标题"})                       # invalid
items.append({"title": "十天前的老新闻", "content": "过期", "category": "tech",
              "publishedAt": (now - timedelta(days=10)).isoformat()})  # expired

st, r = call("POST", "/api/open/v1/news/batch", key=ingest_key,
             body={"source": "tech-daily", "items": items})
d = r.get("data", {})
check("导入成功", st == 200, r)
check("20 条入库", d.get("accepted") == 20, d)
check("1 条无效（标题空）", d.get("invalid") == 1, d)
check("1 条过期被拒", d.get("expired") == 1, d)
check("来源白名单：不存在的来源整批拒绝", True, d)  # 下方单独验证
st2, r2 = call("POST", "/api/open/v1/news/batch", key=ingest_key,
               body={"source": "not-configured", "items": items[:1]})
check("未配置数据源被 403 拒绝", st2 == 403, r2.get("message"))
check("错误明细带下标", bool(d.get("errors")) and d["errors"][0]["index"] == 20, d.get("errors"))

st, r = call("POST", "/api/open/v1/news/batch", key=ingest_key,
             body={"source": "tech-daily", "items": items[:20]})
d = r.get("data", {})
check("重复推送全部跳过", d.get("accepted") == 0 and d.get("duplicate") == 20, d)

print("== 开放读接口 ==")
st, r = call("GET", "/api/open/v1/news?limit=5", key=read_key)
page = r.get("data", {})
check("客户端密钥可读", st == 200 and len(page.get("list", [])) == 5, page)
check("按发布时间倒序", page["list"][0]["title"] == "测试资讯-00", page["list"][0]["title"])
check("列表不含正文", "content" not in page["list"][0], page["list"][0].keys())
check("返回下一页游标", bool(page.get("nextCursor")) and page.get("hasMore"), page)

st, r2 = call("GET", f"/api/open/v1/news?limit=5&cursor={page['nextCursor']}", key=read_key)
page2 = r2["data"]
ids1 = {x["id"] for x in page["list"]}
ids2 = {x["id"] for x in page2["list"]}
check("游标翻页不重复", not (ids1 & ids2), (len(ids1), len(ids2)))
check("第二页 5 条", len(page2["list"]) == 5, len(page2["list"]))

st, r = call("GET", "/api/open/v1/news?category=finance&limit=50", key=read_key)
check("分类筛选", st == 200 and all(x["category"] == "finance" for x in r["data"]["list"]), r["data"])

st, r = call("GET", "/api/open/v1/news?keyword=" + quote("资讯-07"), key=read_key)
check("关键词检索", st == 200 and len(r["data"]["list"]) == 1, r["data"]["list"])

st, r = call("GET", "/api/open/v1/news/meta", key=read_key)
check("meta 返回分类与来源", st == 200 and r["data"]["total"] == 20 and r["data"]["retentionDays"] == 7, r["data"])

detail_id = page["list"][0]["id"]
st, r = call("GET", f"/api/open/v1/news/{detail_id}", key=read_key)
check("详情带正文", st == 200 and len(r["data"]["content"]) > 0, r["data"].get("id"))

print("== 认证与权限 ==")
st, r = call("GET", "/api/open/v1/news")
check("缺密钥 40107", st == 401 and r.get("code") == 40107, r)
st, r = call("GET", "/api/open/v1/news", key=read_key)
check("只读密钥可读列表", st == 200, r)
st, r = call("POST", "/api/open/v1/news/batch", key=read_key,
             body={"source": "s", "items": [{"title": "x"}]})
check("只读密钥不能写入", st == 403, r)
st, r = call("POST", "/api/open/v1/news/batch", key=ingest_key, body={"source": "s", "items": []})
check("空批次被拒", st == 400, r)

demo = login("demo", "Demo@123456")
st, r = call("GET", "/api/v1/news", demo)
check("普通用户无资讯权限", st == 403, r)

print("== 管理端 ==")
st, r = call("GET", "/api/v1/news?page=1&page_size=10", admin)
check("管理端分页", st == 200 and r["data"]["total"] == 20 and len(r["data"]["list"]) == 10, r["data"])
check("管理端列表不含正文", "content" not in r["data"]["list"][0], r["data"]["list"][0].keys())

st, r = call("GET", "/api/v1/news/stats", admin)
check("统计总量", st == 200 and r["data"]["total"] == 20 and r["data"]["today"] == 20, r["data"])
check("统计分类", len(r["data"]["categories"]) == 2, r["data"]["categories"])

st, r = call("GET", "/api/v1/news-sources", admin)
check("数据源白名单可查询", st == 200 and r["data"]["total"] >= 1 and any(s["code"] == "tech-daily" for s in r["data"]["list"]), r["data"])

st, r = call("POST", "/api/v1/news-sources", admin, {"code": "finance-daily", "name": "财经日报", "kind": "rss"})
check("新建数据源", st == 200 and r["data"]["code"] == "finance-daily", r)
src_id = r["data"]["id"]
st, r = call("POST", "/api/v1/news-sources", admin, {"code": "finance-daily", "name": "重复"})
check("重复编码 409", st == 409, r)
st, r = call("DELETE", f"/api/v1/news-sources/{src_id}", admin)
check("删除空数据源", st == 200, r)

print("== 过期清理 ==")
con = sqlite3.connect(os.environ.get("TENON_NEWS_DB", "/tmp/tenon-news.db"))
con.execute("update news_article set published_at=? where title in ('测试资讯-18','测试资讯-19')",
            ((now - timedelta(days=8)).strftime("%Y-%m-%d %H:%M:%S"),))
con.commit()
con.close()
st, r = call("POST", "/api/v1/news/cleanup", admin)
check("手动清理删除 2 条", st == 200 and r["data"]["deleted"] == 2, r)
st, r = call("GET", "/api/v1/news/stats", admin)
check("清理后总量为 18", r["data"]["total"] == 18, r["data"])

st, r = call("GET", "/api/v1/news?page_size=100", admin)
tid = r["data"]["list"][0]["id"]
st, r = call("DELETE", f"/api/v1/news/{tid}", admin)
check("删除单条", st == 200, r)
st, r = call("GET", f"/api/open/v1/news/{tid}", key=read_key)
check("删除后不可读", st == 404, r)

print(f"\n结果：{ok_count} 通过，{fail_count} 失败")
