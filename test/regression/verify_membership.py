#!/usr/bin/env python3
"""会员体系端到端断言：等级 CRUD、开通/续费/取消、权益、筛选与权限边界。"""
import os
import json
import sys
import urllib.error
import urllib.request
from datetime import date, timedelta

BASE = os.environ.get("KEEL_BASE", "http://127.0.0.1:9101/api/v1").rstrip("/")
PASS, FAIL = 0, 0


def call(method, path, token=None, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw or "{}")
        except json.JSONDecodeError:
            return e.code, {"raw": raw}


def check(name, cond, detail=""):
    global PASS, FAIL
    if cond:
        PASS += 1
        print(f"  ok   {name}")
    else:
        FAIL += 1
        print(f"  FAIL {name} {detail}")


def login(username, password):
    st, res = call("POST", "/auth/login", body={"username": username, "password": password})
    assert st == 200 and res.get("code") == 0, f"登录失败 {username}: {res}"
    return res["data"]["token"]["accessToken"]


admin = login("admin", os.environ.get("KEEL_ADMIN_PASS", "Admin@123456"))
demo = login("demo", "Demo@123456")
print("== 等级目录 ==")
st, res = call("GET", "/membership/catalog", admin)
levels = res.get("data") or []
check("catalog 返回内置 3 个等级", st == 200 and len(levels) == 3, res)
codes = [l["code"] for l in levels]
check("等级按权重从高到低", codes[:1] == ["platinum"], codes)
gold = next(l for l in levels if l["code"] == "gold")
silver = next(l for l in levels if l["code"] == "silver")

print("== 我的会员（非会员）==")
st, res = call("GET", "/membership/me", demo)
me = res.get("data") or {}
check("默认非会员", st == 200 and me.get("member") is False and me.get("state") == "none", me)
check("/me 返回等级目录", len(me.get("levels") or []) == 3)
check("非会员没有权益", me.get("benefits") == {}, me.get("benefits"))

print("== 等级 CRUD ==")
payload = {
    "code": "diamond",
    "name": "钻石会员",
    "rank": 40,
    "status": 1,
    "description": "测试等级",
    "defaultDays": 90,
    "color": "#2E90FA",
    "benefits": [
        {"key": "report.export", "label": "导出报表", "value": "true"},
        {"key": "project.max", "label": "项目数量", "value": "999"},
    ],
}
st, res = call("POST", "/membership/levels", admin, payload)
check("创建等级", st == 200 and res.get("data", {}).get("code") == "diamond", res)
diamond = res.get("data") or {}

st, res = call("POST", "/membership/levels", admin, payload)
check("重复编码返回 409", st == 409, (st, res))

st, res = call("POST", "/membership/levels", admin, {**payload, "code": "BAD-CODE"})
check("非法编码被拒", st == 400, (st, res))

st, res = call("POST", "/membership/levels", admin, {
    **payload, "code": "dupbenefit",
    "benefits": [{"key": "a.b", "label": "A", "value": "1"}, {"key": "a.b", "label": "B", "value": "2"}],
})
check("重复权益标识被拒", st == 400, (st, res))

st, res = call("PUT", f"/membership/levels/{diamond['id']}", admin, {**payload, "name": "钻石会员Plus"})
check("更新等级", st == 200, res)
st, res = call("GET", "/membership/levels?page=1&page_size=20&keyword=diamond", admin)
row = (res.get("data", {}).get("list") or [{}])[0]
check("更新已生效", row.get("name") == "钻石会员Plus", row)
check("列表返回有效会员数", "userCount" in row, row)

print("== 开通 / 续费 ==")
st, res = call("GET", "/users?page=1&page_size=10&keyword=demo", admin)
demo_row = (res["data"]["list"] or [{}])[0]
demo_id = demo_row["id"]
check("用户列表返回非会员状态", demo_row.get("membershipState") == "none", demo_row)

st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": gold["id"], "days": 10})
snap = res.get("data") or {}
check("开通黄金会员", st == 200 and snap.get("member") is True and snap.get("daysLeft") == 10, res)
check("权益快照正确", snap.get("benefits", {}).get("support.priority") == "true", snap.get("benefits"))

st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": gold["id"], "days": 5})
check("同等级续费顺延至 15 天", res.get("data", {}).get("daysLeft") == 15, res)

st, res = call("GET", "/membership/me", demo)
me = res.get("data") or {}
check("用户侧看到会员状态", me.get("member") is True and me.get("level", {}).get("code") == "gold", me)
check("用户侧剩余天数", me.get("daysLeft") == 15, me.get("daysLeft"))
check("用户侧权益可读", me.get("benefits", {}).get("report.export") == "true")

st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": diamond["id"], "permanent": True})
check("切换等级为长期", res.get("data", {}).get("daysLeft") == -1 and res.get("data", {}).get("level", {}).get("code") == "diamond", res)

st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": gold["id"], "expireAt": "2020-01-01"})
check("过去的到期日被拒", st == 400, (st, res))

st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": "not-exist", "days": 3})
check("不存在的等级返回 404", st == 404, (st, res))

tomorrow = (date.today() + timedelta(days=1)).isoformat()
st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": silver["id"], "expireAt": tomorrow})
check("指定日期开通", st == 200 and res.get("data", {}).get("expireAt", "").startswith(tomorrow), res)

print("== 用户筛选 ==")
st, res = call("GET", "/users?page=1&page_size=50&membershipState=member", admin)
names = [u["username"] for u in res["data"]["list"]]
check("有效会员筛选包含 demo", "demo" in names, names)
check("有效会员筛选不含 admin", "admin" not in names, names)

st, res = call("GET", "/users?page=1&page_size=50&membershipState=none", admin)
names = [u["username"] for u in res["data"]["list"]]
check("非会员筛选包含 admin", "admin" in names and "demo" not in names, names)

st, res = call("GET", f"/users?page=1&page_size=50&membershipLevelId={silver['id']}", admin)
names = [u["username"] for u in res["data"]["list"]]
check("按等级筛选", names == ["demo"], names)

print("== 权限边界 ==")
st, res = call("GET", "/membership/levels", demo)
check("普通用户不能管理等级", st == 403, (st, res))
st, res = call("PUT", f"/users/{demo_id}/membership", demo, {"levelId": gold["id"], "days": 3})
check("普通用户不能给自己开会员", st == 403, (st, res))
st, res = call("GET", "/membership/me", demo)
check("普通用户可读自己的会员信息", st == 200, res)

print("== 删除保护 ==")
st, res = call("DELETE", f"/membership/levels/{silver['id']}", admin)
check("有用户引用时拒绝删除", st == 409, (st, res))

print("== 取消会员 ==")
st, res = call("DELETE", f"/users/{demo_id}/membership", admin)
check("取消会员", st == 200, res)
st, res = call("GET", "/membership/me", demo)
check("取消后回到非会员", res.get("data", {}).get("member") is False, res)
st, res = call("DELETE", f"/users/{demo_id}/membership", admin)
check("重复取消幂等", st == 200, res)

print("== 停用等级 ==")
st, res = call("PUT", f"/membership/levels/{diamond['id']}", admin, {**payload, "name": "钻石会员Plus", "status": 0})
check("停用等级", st == 200, res)
st, res = call("PUT", f"/users/{demo_id}/membership", admin, {"levelId": diamond["id"], "days": 3})
check("停用等级不可开通", st == 400, (st, res))
st, res = call("DELETE", f"/membership/levels/{diamond['id']}", admin)
check("无引用可删除", st == 200, res)

print(f"\n通过 {PASS}，失败 {FAIL}")
sys.exit(1 if FAIL else 0)
