# 贡献指南

欢迎提交缺陷修复、文档改进与通用能力增强。本项目定位是**企业服务端基础框架**，
请优先沉淀可复用的通用能力，业务逻辑请放在下游项目。

## 开发前

1. 阅读 [架构设计](docs/architecture.md) 与 [开发指南](docs/development.md)。
2. 重大设计取舍先开 issue 讨论；确认后在 `docs/decisions.md` 追加决策记录。
3. 本地用 `make run` 启动（默认 SQLite + 进程内缓存，零外部依赖）。

## 改动流程

1. 从 `main` 切分支（通用能力可直接提交 main，见决策 #1）：
   `git checkout -b feat/xxx` 或 `fix/xxx`。
2. 按 [开发指南](docs/development.md) 的层次实现：模型 → 服务 → 处理器 → 路由 → 权限点 → 菜单 → 前端 → 测试。
3. 提交前跑：

   ```bash
   make verify          # vet + 架构检查 + 单元测试
   make docs            # 改了路由时必须重新生成接口文档并提交
   ```

4. 里程碑改动额外跑：

   ```bash
   make test-dialect    # 跨方言（需 Docker）
   make test-regression # 接口回归（起服务 + mock）
   ```

5. 提交信息遵循约定式前缀（`feat:` / `fix:` / `docs:` / `test:` / `chore:`），
   正文说明「为什么改」与影响面，详情见开发指南第 10 章。

## 架构铁律（由 `make check-arch` 强制）

- `modules → platform → pkg` 单向依赖，`pkg` 不反向依赖。
- `internal/platform/**` 与 `pkg/**` 不得 import `internal/modules/**`。
- 方言/驱动字面量判断只在白名单文件（驱动选择层与备份模块）。
- 修改 `pkg/**` 公开导出后运行 `python3 scripts/check_architecture.py --update` 更新快照。

## PR 要求

- 关联 issue；按 PR 模板填写校验项。
- 一个 PR 只做一件事；格式化与逻辑改动分开。
- CI 全绿（后端、前端、文档幂等）方可合并。
- 不引入运行时 Node 依赖（前端产物用 `go:embed` 打入二进制）。

## 许可

提交即表示同意你的贡献以 [MIT 许可](LICENSE) 发布。
