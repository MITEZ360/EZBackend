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
PREFIX = "github.com/keel-framework/keel/"

TAGS = {
    "auth": "认证",
    "user": "用户与权限", "role": "用户与权限", "menu": "用户与权限",
    "dept": "用户与权限", "membership": "用户与权限", "apikey": "用户与权限",
    "news": "内容",
    "system": "配置", "message": "配置", "dashboard": "运维",
    "audit": "运维", "job": "运维", "file": "运维",
    "monitor": "运维", "maintenance": "运维",
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


def find_function(directory, typ, handler):
    pat = re.compile(r"^func \([^)]*\*" + re.escape(typ) + r"\)\s+" + re.escape(handler) + r"\(", re.M)
    for fp in sorted(glob.glob(os.path.join(ROOT, directory, "*.go"))):
        if pat.search(open(fp, encoding="utf-8").read()):
            return fp
    return None


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
    routes = collect_routes(fields, router_text)
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
        fp = find_function(r["dir"], r["typ"], r["handler"])
        if not fp:
            stats["missing"] += 1
            missing.append((r["dir"], r["typ"], r["handler"]))
            continue
        lines = cache.setdefault(fp, strip_generated(open(fp, encoding="utf-8").read().splitlines()))
        idx = None
        pat = re.compile(r"^func \([^)]*\*" + re.escape(r["typ"]) + r"\)\s+" + re.escape(r["handler"]) + r"\(")
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
