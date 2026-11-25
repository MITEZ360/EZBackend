#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""从路由注册代码生成 swag 注解，保证接口文档与真实路由一一对应。

用法（项目根目录）：python3 scripts/gen_swagger_annotations.py
生成后执行 `make docs` 生成 OpenAPI 描述。

约定：注解块紧贴在 handler 函数声明上方；已存在 @Router 的函数会跳过，
因此重复执行是幂等的。注解区间以 KE:SWAGGER 标记，方便后续整体重生成。
"""
import glob
import os
import re

ROOT = os.getcwd()
ROUTER = os.path.join(ROOT, "internal", "router")
MODULES = os.path.join(ROOT, "internal", "modules")
PREFIX = "github.com/MITEZ360/tenon/"

TAGS = {
    "auth": "认证", "cnauth": "认证",
    "user": "用户与权限", "role": "用户与权限", "menu": "用户与权限",
    "dept": "用户与权限", "membership": "用户与权限", "apikey": "用户与权限",
    "recharge": "用户与权限",
    "news": "内容",
    "flow": "流程",
    "system": "配置", "dict": "配置", "notice": "配置", "message": "配置",
    "configcenter": "配置",
    "dashboard": "运维", "audit": "运维", "job": "运维", "file": "运维",
    "monitor": "运维", "maintenance": "运维", "cacheadmin": "运维",
    "routemanage": "运维", "cluster": "运维",
}

ROUTE_RE = re.compile(
    r'^\s*(\w+)\.(GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]+)"\s*,\s*(.*)\)\s*$'
)
HANDLER_RE = re.compile(r'\bd\.(\w+)\.(\w+)\b')


def load_deps():
    text = open(os.path.join(ROUTER, "router.go"), encoding="utf-8").read()
    block = re.search(r"type Deps struct \{(.*?)\n\}", text, re.S)
    imports = {}
    for m in re.finditer(r'^\s*(?:(\w+)\s+)?"([^"]+)"', text, re.M):
        alias = m.group(1) or os.path.basename(m.group(2))
        imports[alias] = m.group(2)
    fields = {}
    for m in re.finditer(r"^\s*(\w+)\s+\*(\w+)\.(\w+)\s*$", block.group(1), re.M):
        field, alias, typ = m.group(1), m.group(2), m.group(3)
        path = imports.get(alias, "")
        if path.startswith(PREFIX):
            fields[field] = (path[len(PREFIX):], typ)
    return fields, text


def public_range(text):
    lines = text.splitlines()
    start = None
    for i, line in enumerate(lines):
        if line.startswith("func (d Deps) publicRoutes"):
            start = i
        elif start is not None and i > start and line.startswith("func "):
            return start, i
    return start, len(lines)


def collect_routes(fields, router_text):
    routes = []
    files = sorted(glob.glob(os.path.join(ROUTER, "routes_*.go"))) + [os.path.join(ROUTER, "router.go")]
    prange = public_range(router_text)
    for fp in files:
        is_router = fp.endswith("router.go")
        lines = open(fp, encoding="utf-8").read().splitlines()
        for i, line in enumerate(lines):
            m = ROUTE_RE.match(line)
            if not m:
                continue
            var, method, path, rest = m.group(1), m.group(2), m.group(3), m.group(4)
            hits = HANDLER_RE.findall(rest)
            if not hits:
                continue
            field, handler = hits[-1]
            if is_router and var == "open":
                prefix, group = "/api/open/v1", "open"
            elif is_router and prange[0] is not None and prange[0] <= i < prange[1]:
                prefix, group = "/api/v1", "public"
            elif is_router:
                continue
            else:
                prefix, group = "/api/v1", "authed"
            if field not in fields:
                continue
            directory, typ = fields[field]
            tag = "开放接口" if group == "open" else TAGS.get(os.path.basename(directory), "运维")
            security = {"open": "ApiKeyAuth", "authed": "BearerAuth", "public": ""}[group]
            routes.append({
                "method": method, "path": prefix + path, "typ": typ,
                "handler": handler, "dir": directory, "tag": tag, "security": security,
            })
    return routes


def annotation(route):
    path = re.sub(r":(\w+)", r"{\1}", route["path"])
    params = re.findall(r":(\w+)", route["path"])
    out = [
        "// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md",
        "// @Summary " + route["handler"],
        "// @Tags " + route["tag"],
        "// @Accept json",
        "// @Produce json",
    ]
    for p in params:
        out.append(f'// @Param {p} path string true "{p}"')
    if route["method"] in ("POST", "PUT", "PATCH"):
        out.append('// @Param body body map[string]interface{} false "请求参数"')
    out.append("// @Success 200 {object} map[string]interface{}")
    for code in (400, 401, 403, 404, 409, 429, 500):
        out.append(f"// @Failure {code} {{object}} map[string]interface{{}}")
    if route["security"]:
        out.append("// @Security " + route["security"])
    out.append("// @Router %s [%s]" % (path, route["method"].lower()))
    out.append("// KE:SWAGGER-END")
    return out


MODULE_ROUTE_RE = re.compile(
    r'^\s*(\w+)\.(GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]+)"\s*,\s*(.*)\)\s*$'
)
CTOR_RE = re.compile(r"^(\w+)\s*:=\s*New(\w*Handler)\(")
SETUP_RE = re.compile(r"^func\s+setup\w*\(")
HANDLER_TAIL_RE = re.compile(r"\b(\w+)\.(\w+)\s*\)*\s*$")


def collect_module_routes():
    """模块自带路由：internal/modules/<name>/module.go 的 c.Authed/Public/Open 分组。

    模块化之后业务路由不再集中写在 internal/router，文档生成得跟着模块目录走，
    否则新模块的接口在 swagger 里永远缺席。同一个 setup 里的 h 可能是
    ConfigHandler 也可能是 NoticeHandler，所以按 NewXxxHandler 的局部变量记类型，
    而不是一律当成 Handler。
    """
    routes = []
    for fp in sorted(glob.glob(os.path.join(MODULES, "*", "module.go"))):
        mod = os.path.basename(os.path.dirname(fp))
        directory = "internal/modules/" + mod
        group, typ_of = None, {}
        for line in open(fp, encoding="utf-8").read().splitlines():
            s = line.strip()
            if SETUP_RE.match(s):
                group, typ_of = None, {}
                continue
            m = CTOR_RE.match(s)
            if m:
                typ_of[m.group(1)] = m.group(2)
                continue
            if group is None:
                m = re.match(r"c\.(Authed|Public|Open)\(", s)
                if m:
                    group = m.group(1).lower()
                    continue
            elif s.startswith("})"):
                group = None
                continue
            if group is None:
                continue
            m = MODULE_ROUTE_RE.match(line)
            if not m:
                continue
            var, method, path, rest = m.group(1), m.group(2), m.group(3), m.group(4)
            hits = HANDLER_TAIL_RE.findall(rest)
            if not hits:
                continue
            recv, handler = hits[-1]
            # 开放接口走 /api/open/v1 与 API 密钥认证，标签单独归类，
            # 否则密钥签发与密钥调用会混进同一个模块分组。
            open_group = group == "open"
            routes.append({
                "method": method,
                "path": ("/api/open/v1" if open_group else "/api/v1") + path,
                "typ": typ_of.get(recv, "Handler"),
                "handler": handler, "dir": directory,
                "tag": "开放接口" if open_group else TAGS.get(mod, "业务"),
                "security": {"authed": "BearerAuth", "public": "", "open": "ApiKeyAuth"}[group],
            })
    return routes


def find_function(directory, typ, handler):
    pat = re.compile(r"^func \([^)]*\*" + re.escape(typ) + r"\)\s+" + re.escape(handler) + r"\(", re.M)
    for fp in sorted(glob.glob(os.path.join(ROOT, directory, "*.go"))):
        if pat.search(open(fp, encoding="utf-8").read()):
            return fp
    return None
def find_function_any(directory, handler):
    """按 handler 名在模块目录里找实现，只认接收者类型以 Handler 结尾的方法。

    路由注册里只有 h.XXX，类型要自己认：ConfigHandler / DictHandler /
    OpenHandler 这些都在同一目录。同名方法命中多个类型时不猜，留给人工。
    """
    pat = re.compile(r"^func \([^)]*\*(\w*Handler)\)\s+" + re.escape(handler) + r"\(", re.M)
    found = set()
    for fp in sorted(glob.glob(os.path.join(ROOT, directory, "*.go"))):
        for m in pat.finditer(open(fp, encoding="utf-8").read()):
            found.add((fp, m.group(1)))
    if len(found) == 1:
        return found.pop()
    return None, None


def strip_generated(lines):
    """移除旧的 KE:SWAGGER 块，支持整体重生成。"""
    out, skip = [], False
    for line in lines:
        if "KE:SWAGGER-BEGIN" in line:
            skip = True
            continue
        if "KE:SWAGGER-END" in line:
            skip = False
            continue
        if not skip:
            out.append(line)
    return out


def main():
    fields, router_text = load_deps()
    routes = collect_routes(fields, router_text) + collect_module_routes()
    cache = {}
    stats = {"ok": 0, "missing": 0, "duplicate": 0}
    missing = []
    seen = set()
    for r in routes:
        # swag 一个函数只能对应一个 @Router；同一 handler 被多条路由复用时只取第一条
        key = (r["dir"], r["typ"], r["handler"])
        if key in seen:
            stats["duplicate"] += 1
            continue
        seen.add(key)
        typ = r["typ"]
        fp = find_function(r["dir"], typ, r["handler"])
        if not fp:
            fp, typ = find_function_any(r["dir"], r["handler"])
        if not fp:
            stats["missing"] += 1
            missing.append((r["dir"], r["typ"], r["handler"]))
            continue
        lines = cache.setdefault(fp, strip_generated(open(fp, encoding="utf-8").read().splitlines()))
        idx = None
        pat = re.compile(r"^func \([^)]*\*" + re.escape(typ) + r"\)\s+" + re.escape(r["handler"]) + r"\(")
        for i, line in enumerate(lines):
            if pat.match(line):
                idx = i
                break
        if idx is None:
            stats["missing"] += 1
            missing.append((fp, r["typ"], r["handler"]))
            continue
        # 已存在其它 @Router 注解时不重复插入
        upper = "\n".join(lines[max(0, idx - 25):idx])
        if "@Router " in upper and "KE:SWAGGER" not in upper:
            stats["duplicate"] += 1
            continue
        lines[idx:idx] = annotation(r)
        stats["ok"] += 1
    for fp, lines in cache.items():
        open(fp, "w", encoding="utf-8").write("\n".join(lines).rstrip() + "\n")
    print("路由 %d 条，生成注解 %d，跳过已有 %d，未找到 %d" %
          (len(routes), stats["ok"], stats["duplicate"], stats["missing"]))
    for m in missing:
        print("  未找到:", m)


if __name__ == "__main__":
    main()
