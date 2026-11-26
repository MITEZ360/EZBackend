/**
 * 发行版标识：构建时由 VITE_EDITION 注入。
 *
 * - `base`：只含后台管理基础能力（make dist-base）
 * - `gov` ：Base + 服务治理三件套（make dist-gov）
 * - 未注入 / `default`：完整版，可选模块全在
 *
 * 精简发行版（base / gov）不含会员、充值、资讯、开放密钥这些业务模块，
 * 页面上的相关入口既不渲染也不请求——后端那些接口连代码都没有，
 * 留着入口只会在点击时 404。
 */
const EDITION = import.meta.env.VITE_EDITION ?? 'default'

/** 治理面独立发行版：Base + 注册发现 + 动态路由 + 配置中心。 */
export const IS_GOV = EDITION === 'gov'

/**
 * 精简版：不含会员/充值/资讯/开放密钥等可选业务。
 *
 * 治理面同样是精简版——它只多出治理模块，业务模块一个都没有，
 * 所以沿用同一套隐藏逻辑，不必让每个页面再判一次发行版。
 */
export const IS_BASE = EDITION === 'base' || IS_GOV
