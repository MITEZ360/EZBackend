# 接口回归脚本

Python 标准库编写的接口级回归（无需 pytest / requests）。此前散落在临时目录，
现统一入库，可一键复现。

## 一键运行

```bash
make test-regression
# 或
bash test/regression/run_all.sh
```

`run_all.sh` 会：编译最新二进制 → 起干净的主服务(9000)、mock OAuth provider(9101)、
OAuth 演示实例(9002) → 等待就绪 → 按序跑全部脚本 → 清理进程与临时文件。
每次使用独立的临时数据库与上传目录，不污染开发库。

依赖：`go`、`python3`、`curl`。跨实例的 OAuth 流程依赖 `mock_oauth.py`。

## 脚本清单

| 脚本 | 覆盖 | 默认 BASE | 断言数 |
| --- | --- | --- | --- |
| `verify_membership.py` | 会员等级 CRUD、开通/续费/取消、权益、筛选、权限边界 | `KEEL_BASE/api/v1` | 23 |
| `verify_apikey.py` | API 密钥签发、scope、IP 白名单、配额、轮换、状态 | `KEEL_BASE` | 23 |
| `verify_news.py` | 资讯批量导入、去重、游标分页、检索、清理 | `KEEL_BASE` | 37 |
| `verify_news_taxonomy.py` | 来源/分类白名单、非法分类单条 invalid、图标下发 | `KEEL_BASE` | 23 |
| `verify_ops.py` | 备份（含文件资产）、校验、dry-run、GFS、迁移、Swagger、OAuth 未启用 | `KEEL_BASE` | 35 |
| `verify_oauth.py` | 配置化 OAuth：字段映射、email_url、自动注册、绑定/解绑、state 防重放 | `KEEL_BASE` | 18 |

合计 **136 项断言**。

## 单独运行

脚本支持通过环境变量覆盖目标，便于连到任意实例：

```bash
KEEL_BASE=http://127.0.0.1:9000 python3 test/regression/verify_ops.py
KEEL_BASE=http://127.0.0.1:9000/api/v1 python3 test/regression/verify_membership.py
KEEL_ADMIN_PASS='你的密码' python3 test/regression/verify_apikey.py
# verify_news.py 需要直接改库验证过期清理，用 KEEL_NEWS_DB 指定 SQLite 文件
KEEL_NEWS_DB=data/keel.db python3 test/regression/verify_news.py
```

| 环境变量 | 说明 | 默认 |
| --- | --- | --- |
| `KEEL_BASE` | 服务根地址 | 各脚本原端口（9000/9002） |
| `KEEL_ADMIN_PASS` | 管理员密码 | `Admin@123456` |
| `KEEL_NEWS_DB` | 资讯脚本直连的 SQLite 文件 | `/tmp/keel-news.db` |

> 注意：`verify_membership.py` 的 BASE 需带 `/api/v1` 后缀，其余脚本用纯 host。
> `run_all.sh` 已为各脚本传正确的 BASE。

## 与 Go 单测的分工

- **Go 单测**（`go test ./...`）：纯逻辑、模型服务、启动自检、模块注册表、归档打包、
  GFS 分档、迁移 up/down、OAuth 字段映射——快、无外部依赖，日常与 CI 必跑。
- **Python 接口回归**（本目录）：真实 HTTP 链路、跨模块协同、OAuth 浏览器式回调流程——
  里程碑与发版前跑。

两者互补：Go 单测保证单元正确，接口回归保证装配与协作正确。
