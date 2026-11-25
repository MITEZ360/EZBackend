#!/usr/bin/env python3
"""架构约束机器检查（P1-1）。

三条规则：
  1. internal/platform/** 与 pkg/** 不得 import internal/modules/**（内核不反向依赖业务）
  2. 数据库方言/驱动字面量判断只允许出现在白名单文件（防止业务模块散落方言分支）
  3. pkg/** 公开导出签名快照：变更必须显式 --update（防止偷改公共 API）

用法：
  python3 scripts/check_architecture.py            # 检查，违规非零退出
  python3 scripts/check_architecture.py --update   # 重新生成 pkg 签名快照

设计说明：规则 2 不做「内核不得出现业务词」的静态黑名单（在 Go 里 category/order
等是中性词，会误伤），改为更贴合 Go 项目的 import 边界 + 方言判断白名单。
"""
from __future__ import annotations

import hashlib
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SNAPSHOT = ROOT / "pkg" / ".api-snapshot.json"

# 允许出现方言/驱动字面量判断的文件（相对仓库根）。
# 这些位置要么本身就是驱动选择层，要么必须按驱动分支：
#   - config：ResolveDriver / 校验 / 默认值
#   - database：GORM dialector 选择
#   - migrate：goose dialect 映射
#   - platform/backup：备份必须按驱动分支（转储/恢复工具不同）
#   - bootstrap/runtime：启动期按驱动做 SQLite 特判
DIALECT_WHITELIST = {
    "internal/platform/config/config.go",
    "internal/platform/database/database.go",
    "internal/platform/migrate/migrate.go",
    "internal/platform/backup/backup.go",
    "internal/bootstrap/runtime.go",
}

DIALECT_LITERAL = re.compile(r'"(sqlite|mysql|postgres|postgresql|mariadb)"')
# 只把「出现在比较/分支上下文」的方言字面量算作违规，避免 import 路径、
# 日志文案、配置键里的同名字面量误报。
DIALECT_BRANCH = re.compile(
    r'(==|!=)\s*"'          # x == "sqlite"
    r'|case\s+[^:\n]*"'      # case "sqlite": / case x, "mysql":
    r'|switch\s+[^:\n]*"'    # switch x { 里当场比较（少见的简写）
    r'|strings\.Contains\([^)]*"'  # strings.Contains(driver, "mysql")
)

IMPORT_MODULES = re.compile(r'"github\.com/MITEZ360/tenon/internal/modules(?:/[^"]*)?"')


def go_files(base: Path) -> list[Path]:
    return sorted(p for p in base.rglob("*.go") if not p.name.endswith("_test.go"))


def rel(p: Path) -> str:
    return p.relative_to(ROOT).as_posix()


def rule_import_boundary() -> list[str]:
    violations: list[str] = []
    scopes = [ROOT / "internal" / "platform", ROOT / "pkg"]
    for base in scopes:
        for p in go_files(base):
            for no, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
                if IMPORT_MODULES.search(line):
                    violations.append(f"[import-boundary] {rel(p)}:{no} 内核/公共包不得 import 业务模块: {line.strip()}")
    return violations


def rule_dialect_whitelist() -> list[str]:
    violations: list[str] = []
    for base in [ROOT / "internal", ROOT / "pkg", ROOT / "cmd"]:
        for p in go_files(base):
            r = rel(p)
            if r in DIALECT_WHITELIST:
                continue
            for no, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
                s = line.strip()
                if s.startswith("//"):
                    continue
                if DIALECT_LITERAL.search(line) and DIALECT_BRANCH.search(line):
                    violations.append(f"[dialect-whitelist] {r}:{no} 方言判断只允许出现在白名单文件: {s}")
    return violations


# pkg 导出签名提取：捕获顶层 func / type / const / var 声明（含方法接收者）。
EXPORT_RE = re.compile(
    r"^(func\s+(?:\([^)]*\)\s*)?[A-Z]\w*"
    r"|type\s+[A-Z]\w*"
    r"|const\s+[A-Z]\w*"
    r"|var\s+[A-Z]\w*)",
    re.MULTILINE,
)


def build_snapshot() -> dict:
    result: dict[str, list[str]] = {}
    for p in go_files(ROOT / "pkg"):
        decls: list[str] = []
        for line in p.read_text(encoding="utf-8").splitlines():
            s = line.strip()
            if s.startswith("//"):
                continue
            m = EXPORT_RE.match(s)
            if m:
                # 归一化空白与行尾，忽略具体函数体
                decls.append(re.sub(r"\s+", " ", s).rstrip(" {"))
        if decls:
            result[rel(p)] = sorted(decls)
    return result


def rule_pkg_snapshot(update: bool) -> list[str]:
    current = build_snapshot()
    if update:
        SNAPSHOT.write_text(json.dumps(current, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        print(f"已更新 pkg 导出签名快照: {rel(SNAPSHOT)}（{sum(len(v) for v in current.values())} 项）")
        return []
    if not SNAPSHOT.exists():
        return [f"[pkg-snapshot] 缺少快照 {rel(SNAPSHOT)}，请运行: python3 scripts/check_architecture.py --update"]
    old = json.loads(SNAPSHOT.read_text(encoding="utf-8"))
    violations: list[str] = []
    for path in sorted(set(old) | set(current)):
        before, after = old.get(path, []), current.get(path, [])
        if before == after:
            continue
        removed = [d for d in before if d not in after]
        added = [d for d in after if d not in before]
        detail = []
        if removed:
            detail.append("移除: " + "; ".join(removed[:3]))
        if added:
            detail.append("新增: " + "; ".join(added[:3]))
        violations.append(f"[pkg-snapshot] {path} 导出签名变更（{ ' | '.join(detail) }）；确认后运行 --update")
    return violations


def main() -> int:
    update = "--update" in sys.argv[1:]
    violations: list[str] = []
    violations += rule_import_boundary()
    violations += rule_dialect_whitelist()
    violations += rule_pkg_snapshot(update)
    if violations:
        print(f"架构约束检查失败（{len(violations)} 项）：", file=sys.stderr)
        for v in violations:
            print("  " + v, file=sys.stderr)
        return 1
    print("架构约束检查通过（import 边界 / 方言白名单 / pkg 签名快照）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
