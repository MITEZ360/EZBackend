<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { cnApi, type PaymentConfigView, type RechargeOrder } from '@/api/cn'
import { membershipApi } from '@/api/membership'
import type { MembershipLevel } from '@/api/types'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const levels = ref<MembershipLevel[]>([])
const channels = ref<string[]>([])
const levelId = ref('')
const payType = ref<'wechat' | 'alipay'>('wechat')
const order = ref<RechargeOrder | null>(null)
const loading = ref(false)
const polling = ref<ReturnType<typeof setInterval> | null>(null)
const configVisible = ref(false)
const savingConfig = ref(false)
const payConfig = ref<PaymentConfigView | null>(null)

const selected = computed(() => levels.value.find((lv) => lv.id === levelId.value))
const payable = computed(() => levels.value.filter((lv) => (lv.price ?? 0) > 0 && lv.status === 1))
const payURL = computed(() => order.value?.payUrl || order.value?.qrCode || '')
const amountText = computed(() =>
  selected.value ? `¥${((selected.value.price ?? 0) / 100).toFixed(2)}` : '',
)

onMounted(async () => {
  const [catalog, ch] = await Promise.all([membershipApi.catalog(), cnApi.channels()])
  levels.value = catalog
  channels.value = ch.channels
  if (payable.value.length > 0) levelId.value = payable.value[0].id
})

onUnmounted(() => stopPolling())

function stopPolling() {
  if (polling.value) clearInterval(polling.value)
  polling.value = null
}

function watchOrder(orderNo: string) {
  stopPolling()
  polling.value = setInterval(async () => {
    try {
      const latest = await cnApi.queryOrder(orderNo)
      order.value = latest
      if (latest.status === 'paid') {
        stopPolling()
        ElMessage.success(t('支付成功，会员已到账'))
        await membershipApi.me()
      } else if (latest.status === 'closed' || latest.status === 'refunded') {
        stopPolling()
      }
    } catch {
      stopPolling()
    }
  }, 2500)
}

async function createOrder() {
  if (!levelId.value) {
    ElMessage.warning(t('请选择会员等级'))
    return
  }
  loading.value = true
  try {
    const created = await cnApi.createOrder({ levelId: levelId.value, payType: payType.value })
    order.value = created
    watchOrder(created.orderNo)
  } finally {
    loading.value = false
  }
}

function channelLabel(type: string) {
  return type === 'wechat' ? t('微信支付') : t('支付宝')
}

function statusLabel(status: string) {
  if (status === 'paid') return t('已支付')
  if (status === 'closed') return t('已关闭')
  if (status === 'refunded') return t('已退款')
  return t('待支付')
}

async function openConfig() {
  try {
    payConfig.value = await cnApi.paymentConfig()
    configVisible.value = true
  } catch {
    // 请求拦截器已提示错误
  }
}

async function saveConfig() {
  if (!payConfig.value) return
  savingConfig.value = true
  try {
    await cnApi.savePaymentConfig(payConfig.value)
    ElMessage.success(t('支付配置已保存并立即生效'))
    configVisible.value = false
    channels.value = (await cnApi.channels()).channels
    if (channels.value.length > 0 && !channels.value.includes(payType.value)) {
      payType.value = channels.value[0] as 'wechat' | 'alipay'
    }
  } finally {
    savingConfig.value = false
  }
}
</script>

<template>
  <div class="recharge-page">
    <div class="page-head">
      <h2>{{ t('会员充值') }}</h2>
      <el-button v-if="auth.can('system:recharge:config')" size="small" @click="openConfig">
        {{ t('支付配置') }}
      </el-button>
    </div>
    <el-empty v-if="payable.length === 0" :description="t('暂无可购买的会员等级')" />
    <template v-else>
      <div class="levels">
        <button
          v-for="lv in payable"
          :key="lv.id"
          class="level-card"
          :class="{ active: lv.id === levelId }"
          @click="levelId = lv.id"
        >
          <div class="name">{{ lv.name }}</div>
          <div class="price">¥{{ ((lv.price ?? 0) / 100).toFixed(2) }}</div>
          <div class="days">{{ lv.defaultDays > 0 ? `${lv.defaultDays}${t('天')}` : t('长期有效') }}</div>
        </button>
      </div>

      <div class="pay-row">
        <el-radio-group v-model="payType">
          <el-radio v-for="ch in channels" :key="ch" :value="ch">{{ channelLabel(ch) }}</el-radio>
        </el-radio-group>
        <el-button type="primary" :loading="loading" :disabled="channels.length === 0" @click="createOrder">
          {{ t('立即支付') }} {{ amountText }}
        </el-button>
      </div>

      <el-alert
        v-if="channels.length === 0"
        :title="t('暂无可用支付渠道')"
        type="warning"
        :closable="false"
      />

      <el-card v-if="order" class="order-card">
        <p>{{ t('订单号') }}：<code>{{ order.orderNo }}</code></p>
        <p>{{ t('订单状态') }}：{{ statusLabel(order.status) }}</p>
        <p v-if="payURL">
          {{ t('支付链接') }}：
          <a :href="payURL" target="_blank" rel="noopener">{{ payURL }}</a>
        </p>
        <p class="hint">{{ t('支付完成后会自动到账，此页面每 2.5 秒刷新一次订单状态') }}</p>
      </el-card>
    </template>

    <el-dialog v-model="configVisible" :title="t('支付配置')" width="680px" destroy-on-close>
      <template v-if="payConfig">
        <el-alert
          v-if="payConfig.source === 'file'"
          :title="t('当前使用启动配置；保存后写入数据库并立即生效，无需重启服务')"
          type="info"
          :closable="false"
          class="config-tip"
        />
        <el-form label-width="140px" size="small">
          <el-form-item :label="t('启用支付总开关')">
            <el-switch v-model="payConfig.enabled" />
          </el-form-item>

          <el-divider content-position="left">{{ t('微信支付 Native') }}</el-divider>
          <el-form-item :label="t('启用微信支付')">
            <el-switch v-model="payConfig.wechat.enabled" />
          </el-form-item>
          <el-form-item label="AppID">
            <el-input v-model="payConfig.wechat.appId" />
          </el-form-item>
          <el-form-item label="MchID">
            <el-input v-model="payConfig.wechat.mchId" />
          </el-form-item>
          <el-form-item :label="t('API 密钥')">
            <el-input v-model="payConfig.wechat.apiKey" type="password" show-password />
          </el-form-item>
          <el-form-item :label="t('回调地址')">
            <el-input v-model="payConfig.wechat.notifyUrl" placeholder="/api/v1/pay/notify/wechat" />
          </el-form-item>
          <el-form-item :label="t('签名方式')">
            <el-select v-model="payConfig.wechat.signType" style="width: 180px">
              <el-option label="MD5" value="MD5" />
              <el-option label="HMAC-SHA256" value="HMAC-SHA256" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('沙箱模式')">
            <el-switch v-model="payConfig.wechat.sandbox" />
          </el-form-item>

          <el-divider content-position="left">{{ t('支付宝电脑网站支付') }}</el-divider>
          <el-form-item :label="t('启用支付宝')">
            <el-switch v-model="payConfig.alipay.enabled" />
          </el-form-item>
          <el-form-item label="AppID">
            <el-input v-model="payConfig.alipay.appId" />
          </el-form-item>
          <el-form-item :label="t('应用私钥（PEM）')">
            <el-input v-model="payConfig.alipay.privateKey" type="textarea" :rows="3" />
          </el-form-item>
          <el-form-item :label="t('支付宝公钥（PEM）')">
            <el-input v-model="payConfig.alipay.publicKey" type="textarea" :rows="3" />
          </el-form-item>
          <el-form-item :label="t('回调地址')">
            <el-input v-model="payConfig.alipay.notifyUrl" placeholder="/api/v1/pay/notify/alipay" />
          </el-form-item>
          <el-form-item :label="t('同步跳转地址')">
            <el-input v-model="payConfig.alipay.returnUrl" />
          </el-form-item>
          <el-form-item :label="t('沙箱模式')">
            <el-switch v-model="payConfig.alipay.sandbox" />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="configVisible = false">{{ t('取消') }}</el-button>
        <el-button type="primary" :loading="savingConfig" @click="saveConfig">
          {{ t('保存并立即生效') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.recharge-page { max-width: 860px; margin: 0 auto; padding: 24px; }
.page-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.config-tip { margin-bottom: 16px; }
.levels { display: flex; flex-wrap: wrap; gap: 12px; margin: 20px 0; }
.level-card {
  min-width: 160px; padding: 16px; border: 1px solid var(--el-border-color);
  border-radius: 8px; background: var(--el-bg-color); cursor: pointer; text-align: left;
  &.active { border-color: var(--el-color-primary); box-shadow: 0 0 0 2px var(--el-color-primary-light-8); }
  .name { font-weight: 600; }
  .price { font-size: 22px; color: var(--el-color-danger); margin: 8px 0 4px; }
  .days { color: var(--el-text-color-secondary); font-size: 13px; }
}
.pay-row { display: flex; align-items: center; gap: 20px; flex-wrap: wrap; }
.order-card { margin-top: 24px; p { margin: 6px 0; } .hint { color: var(--el-text-color-secondary); font-size: 13px; } }
</style>
