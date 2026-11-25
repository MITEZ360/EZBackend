/**
 * 发行版标识：构建时由 VITE_EDITION 注入（make dist-base 传 base）。
 *
 * base 版只含后台管理的基础能力，可选模块（会员、充值、资讯、开放密钥、
 * 集群）既不在后端二进制里，也不在前端路由与界面里。未注入时按完整版工作，
 * 便于同一份源码直接 pnpm dev / pnpm build。
 */
export const IS_BASE = import.meta.env.VITE_EDITION === 'base'
