# 中国大陆能力（可选模块）

中国大陆常用的身份接入、短信、支付与会员充值，在 Keel 里是两个**可选模块**：

| 模块 | 内容 | 依赖 |
|---|---|---|
| `cnauth` | 手机号自助注册、短信验证码登录、找回密码 | `auth` |
| `recharge` | 会员等级售价、充值下单、微信/支付宝回调、订单管理 | `membership` |

默认发行版不含它们，`make build` 出来的二进制里没有这部分代码。
需要时按模块组装：

```bash
make dist MODULES="user cnauth recharge"            # 后台基础 + 国内认证 + 充值
make dist MODULES="user configcenter cnauth recharge" # 再加配置中心
```

短信网关（`internal/platform/sms`）与支付网关（`internal/platform/pay`）是平台库，
只在被上述模块引用时进二进制。微信/QQ 的 OAuth 协议适配在 `auth` 模块内部，
由配置里的 `protocol` 字段驱动，默认 `standard` 行为与标准 OAuth2 一致。

所有能力默认关闭：不配置就不开放入口，不影响既有功能。

## 目录

- [配置](#配置)
- [用户注册与短信登录](#用户注册与短信登录)
- [第三方登录](#第三方登录)
- [会员充值](#会员充值)
- [管理端支付配置与服务重启](#管理端支付配置与服务重启)
- [支付回调](#支付回调)
- [安全说明](#安全说明)

## 配置

用 `configs/config.cn.yaml` 启动（它包含 `cn` / `sms` / `payment` 三段的完整示例）：

```bash
make dist MODULES="user cnauth recharge"
./bin/keel -config configs/config.cn.yaml
```

敏感凭据建议用环境变量注入，避免写入配置文件：

| 环境变量 | 说明 |
|---|---|
| `KEEL_CN_REGISTER_ENABLED` | 开放自助注册 |
| `KEEL_CN_SMS_LOGIN_ENABLED` | 开放短信验证码登录 |
| `KEEL_SMS_ACCESS_KEY_ID` | 短信厂商 AccessKeyId / SecretId |
| `KEEL_SMS_ACCESS_KEY_SECRET` | 短信厂商 AccessKeySecret / SecretKey |
| `KEEL_OAUTH_WECHAT_CLIENT_ID` / `_CLIENT_SECRET` | 微信开放平台凭据 |
| `KEEL_OAUTH_QQ_CLIENT_ID` / `_CLIENT_SECRET` | QQ 互联凭据 |
| `KEEL_PAYMENT_WECHAT_API_KEY` | 微信支付 V2 API 密钥 |
| `KEEL_PAYMENT_ALIPAY_PRIVATE_KEY` / `_PUBLIC_KEY` | 支付宝应用私钥与支付宝公钥（PEM） |

配置段说明见 `configs/config.cn.yaml` 内注释。支付参数也可以直接在管理端配置并立即生效，见下方章节；启动配置仅在管理端尚未保存过时作为默认值。

## 用户注册与短信登录

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/public/cn-config` | 注册/短信登录开关 |
| POST | `/api/v1/auth/sms/code` | 发送短信验证码（`scene`: register / login / reset） |
| POST | `/api/v1/auth/register` | 注册，成功直接返回登录态 |
| POST | `/api/v1/auth/login/phone` | 短信验证码登录；手机号未注册且开放注册时自动建号 |
| POST | `/api/v1/auth/password/reset` | 短信验证码重置密码 |

注册请求：

```json
{
  "username": "alice",
  "password": "Example@123",
  "nickname": "Alice",
  "phone": "13800138000",
  "code": "123456",
  "email": "alice@example.com"
}
```

验证码只以 SHA-256 哈希入库，验证后一次性消费；同一手机号 60 秒内只能发一次，每日上限由 `cn.daily_send_limit` 控制。短信网关支持 `mock`（验证码写日志）、`aliyun`、`tencent`，厂商协议用标准库实现。

## 第三方登录

复用标准 OAuth 路由（`/api/v1/oauth/*`），通过 `oauth.providers[].protocol` 选择协议适配器：

- `standard`（默认）：通用 OAuth2/OIDC，零代码接入。
- `wechat`：微信开放平台扫码登录。支持 `#wechat_redirect` 授权后缀、`appid/secret` 令牌参数、用户信息 `openid` 注入、`unionid` 优先的字段映射。
- `qq`：QQ 互联。支持 `fmt=json` 令牌响应、`/oauth2.0/me` 的 JSONP openid 预取、`oauth_consumer_key` 参数注入。

微信字段映射建议：

```yaml
field_map:
  id: unionid       # 未绑定开放平台时自动回退 openid
  nickname: nickname
  avatar: headimgurl
```

QQ 字段映射建议：`id: openid`、`nickname: nickname`、`avatar: figureurl_qq_2`。

绑定、解绑、自动注册、按邮箱关联已有账号等逻辑与标准渠道完全一致。

## 会员充值

管理员先在「会员等级」中为等级设置售价（单位：分，`0` 表示不可购买）。内置示例等级已配置价格。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/recharge/channels` | 已启用的支付渠道 |
| POST | `/api/v1/recharge/orders` | 创建订单，入参 `levelId` / `payType` |
| GET | `/api/v1/recharge/orders` | 我的订单（分页） |
| GET | `/api/v1/recharge/orders/:orderNo` | 查询订单 |
| POST | `/api/v1/recharge/orders/:orderNo/close` | 关闭待支付订单 |
| GET | `/api/v1/recharge/admin/orders` | 管理端全量订单（需 `system:recharge:list`） |
| GET | `/api/v1/recharge/admin/payment-config` | 读取支付配置（需 `system:recharge:config`） |
| PUT | `/api/v1/recharge/admin/payment-config` | 保存支付配置（需 `system:recharge:config`） |

下单返回 `payUrl`（支付宝跳转地址）与 `qrCode`（微信 Native 的 `code_url`）。金额固定取等级配置，订单不接受客户端传入金额或天数，避免低价购买长期会员。

## 管理端支付配置与服务重启

管理端「充值订单」页面提供「支付配置」按钮（需 `system:recharge:config`），可编辑微信 Native 与支付宝电脑网站支付参数。保存后写入 `cn_payment_config` 表并立即生效，下单、回调与渠道列表每次都重新读取配置，**无需重启服务**。未保存过时回退到 `configs/config.cn.yaml` 与环境变量。

敏感字段（API 密钥、应用私钥、支付宝公钥）使用 `security.data_encrypt_key`（缺省回退 `jwt.secret`）做 AES-256-GCM 加密后入库；读取时返回 `******`，提交 `******` 表示保持原值。

运维中心「服务监控」页面提供「重启服务」按钮（需 `ops:monitor:restart`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/monitor/restart` | 触发服务原地重启 |

接口先返回应答，再优雅停机并通过 `syscall.Exec` 重新执行当前可执行文件，PID 不变。Windows 上改为启动新进程后退出，需依赖服务管理器拉起。该操作会中断在途请求，仅限 super 管理员使用；多实例部署需由编排系统逐实例操作。

## 支付回调

| 渠道 | 回调地址 | 验签方式 |
|---|---|---|
| 微信支付 V2 | `POST /api/v1/pay/notify/wechat` | MD5 / HMAC-SHA256（按 `sign_type` 配置） |
| 支付宝 | `POST /api/v1/pay/notify/alipay` | RSA2（SHA256withRSA） |

回调地址为公网可访问路径，需在商户平台登记，并通过 `payment.wechat.notify_url` / `payment.alipay.notify_url` 配置。

幂等处理链路：

1. 校验渠道签名；
2. `cache.Lock("pay:notify:<渠道>:<订单号>")` 防止并发重复处理；
3. 事务内 `UPDATE ... WHERE status='pending'`，仅 `RowsAffected == 1` 时发放会员；
4. 会员发放使用 `membership.Service.GrantTx` 在同一事务完成；
5. 已支付订单直接返回渠道成功应答。

微信支付沙箱仍需手动配置沙箱密钥，代码默认走正式域名；支付宝沙箱使用 `openapi.alipaydev.com`。

## 安全说明

- 注册、短信登录、支付回调均为公开接口，受全局 IP 限流保护；生产环境建议额外配置 WAF 与短信风控。
- 商户私钥、API 密钥不要提交到仓库，使用环境变量注入。
- 多实例部署必须配置 Redis，否则短信验证码频率限制、fail 计数与支付回调锁只在进程内生效。
- 会员发放不处理退款后的回收，退款需在管理端手工撤销会员（`DELETE /api/v1/users/:id/membership`）。

## 验证

```bash
go test ./...
cd web && pnpm typecheck
```
