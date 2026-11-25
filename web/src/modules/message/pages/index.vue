<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Delete, Promotion, Refresh, Search } from '@element-plus/icons-vue'
import { messageApi, type SendMessagePayload } from '@/api/message'
import { roleApi } from '@/api/system'
import { userApi } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import { useMessageStore } from '@/stores/message'
import type { MessageDetail, MessageItem, MessageSentRow, MessageType, RoleRow } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'

const { t } = useI18n()

const auth = useAuthStore()
const store = useMessageStore()

const TYPE_META = computed<Record<MessageType, { label: string; tone: 'primary' | 'success' | 'warning' | 'info' }>>(() => ({
  notice: { label: t('通知'), tone: 'primary' },
  announcement: { label: t('公告'), tone: 'success' },
  warning: { label: t('预警'), tone: 'warning' },
  system: { label: t('系统'), tone: 'info' },
}))

function typeMeta(type: string) {
  return TYPE_META.value[type as MessageType] ?? { label: type || t('通知'), tone: 'info' as const }
}

const tab = ref<'inbox' | 'sent'>('inbox')
const canManage = computed(() => auth.can('system:message:list'))

/* ------------------------------------------------------------ 我的消息 */
const items = ref<MessageItem[]>([])
const total = ref(0)
const loading = ref(false)
const selected = ref<string[]>([])
const query = reactive({
  page: 1,
  page_size: 20,
  status: '' as '' | 'unread' | 'read',
  type: '',
  keyword: '',
})

const allChecked = computed(
  () => items.value.length > 0 && items.value.every((m) => selected.value.includes(m.messageId)),
)

async function loadInbox() {
  loading.value = true
  try {
    const res = await messageApi.inbox({
      page: query.page,
      page_size: query.page_size,
      status: query.status || undefined,
      type: query.type || undefined,
      keyword: query.keyword || undefined,
    })
    items.value = res.list ?? []
    total.value = res.total ?? 0
    // 翻页后丢弃已不在当前页的选择，避免批量删除误伤
    selected.value = selected.value.filter((id) => items.value.some((m) => m.messageId === id))
  } finally {
    loading.value = false
  }
}

function reloadInbox() {
  query.page = 1
  loadInbox()
}

function resetQuery() {
  query.keyword = ''
  query.status = ''
  query.type = ''
  reloadInbox()
}

function toggleAll(checked: boolean) {
  selected.value = checked ? items.value.map((m) => m.messageId) : []
}

/* -------------------------------------------------------------- 详情 */
const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<MessageDetail | null>(null)

async function openDetail(m: MessageItem) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = null
  try {
    detail.value = await messageApi.detail(m.messageId)
    if (!m.readAt) {
      await messageApi.markRead(m.messageId)
      m.readAt = new Date().toISOString()
      await store.refresh()
    }
  } catch {
    ElMessage.error(t('读取消息详情失败'))
  } finally {
    detailLoading.value = false
  }
}

async function markRead(m: MessageItem) {
  if (m.readAt) return
  await messageApi.markRead(m.messageId)
  m.readAt = new Date().toISOString()
  await store.refresh()
}

async function markAllRead() {
  const res = await messageApi.markAllRead(query.type || undefined)
  ElMessage.success(res.updated > 0 ? t('已标记 {count} 条为已读', { count: res.updated }) : t('没有未读消息'))
  await Promise.all([loadInbox(), store.refresh()])
}

async function removeOne(m: MessageItem) {
  try {
    await ElMessageBox.confirm(t('删除后无法恢复，确定删除这条消息？'), t('删除消息'), {
      confirmButtonText: t('删除'),
      cancelButtonText: t('取消'),
      type: 'warning',
    })
  } catch {
    return
  }
  await messageApi.remove(m.messageId)
  ElMessage.success(t('已删除'))
  if (items.value.length === 1 && query.page > 1) query.page -= 1
  await Promise.all([loadInbox(), store.refresh()])
}

async function removeSelected() {
  if (!selected.value.length) return
  try {
    await ElMessageBox.confirm(
      t('将删除选中的 {count} 条消息，删除后无法恢复。', { count: selected.value.length }),
      t('批量删除'),
      { confirmButtonText: t('删除'), cancelButtonText: t('取消'), type: 'warning' },
    )
  } catch {
    return
  }
  const res = await messageApi.batchRemove(selected.value)
  selected.value = []
  ElMessage.success(t('已删除 {count} 条', { count: res.deleted }))
  if (!items.value.length && query.page > 1) query.page -= 1
  await Promise.all([loadInbox(), store.refresh()])
}

async function clearRead() {
  try {
    await ElMessageBox.confirm(t('将删除所有已读消息，未读消息会保留。'), t('清空已读'), {
      confirmButtonText: t('清空'),
      cancelButtonText: t('取消'),
      type: 'warning',
    })
  } catch {
    return
  }
  const res = await messageApi.clearRead()
  ElMessage.success(t('已清空 {count} 条已读消息', { count: res.deleted }))
  if (!items.value.length && query.page > 1) query.page -= 1
  await Promise.all([loadInbox(), store.refresh()])
}

/* ---------------------------------------------------------- 发送记录 */
const sentRows = ref<MessageSentRow[]>([])
const sentTotal = ref(0)
const sentLoading = ref(false)
const sentQuery = reactive({
  page: 1,
  page_size: 10,
  keyword: '',
  type: '',
  status: '' as '' | 'sent' | 'revoked',
})

async function loadSent() {
  sentLoading.value = true
  try {
    const res = await messageApi.sent({
      page: sentQuery.page,
      page_size: sentQuery.page_size,
      keyword: sentQuery.keyword || undefined,
      type: sentQuery.type || undefined,
      status: sentQuery.status || undefined,
    })
    sentRows.value = res.list ?? []
    sentTotal.value = res.total ?? 0
  } finally {
    sentLoading.value = false
  }
}

function reloadSent() {
  sentQuery.page = 1
  loadSent()
}

async function revoke(row: MessageSentRow) {
  try {
    await ElMessageBox.confirm(
      t('撤回后消息会从所有接收人的收件箱移除，且无法恢复。'),
      t('撤回「{title}」', { title: row.title }),
      { confirmButtonText: t('撤回'), cancelButtonText: t('取消'), type: 'warning' },
    )
  } catch {
    return
  }
  await messageApi.revoke(row.id)
  ElMessage.success(t('已撤回'))
  loadSent()
}

function readRate(row: MessageSentRow) {
  if (!row.total) return 0
  return Math.round((row.read / row.total) * 100)
}

function audienceLabel(v: string) {
  return { all: t('全员'), role: t('按角色'), user: t('指定用户') }[v] ?? v
}

function switchTab(name: string | number) {
  if (name === 'sent' && !sentRows.value.length) loadSent()
}

/* ---------------------------------------------------------- 发送消息 */
const sendVisible = ref(false)
const sendRef = ref<FormInstance>()
const sending = ref(false)
const roles = ref<RoleRow[]>([])
const users = ref<{ id: string; username: string; nickname: string }[]>([])
const optionsLoaded = ref(false)

const sendForm = reactive<SendMessagePayload>({
  title: '',
  content: '',
  type: 'notice',
  link: '',
  audience: 'all',
  roleIds: [],
  userIds: [],
})

const sendRules = computed<FormRules>(() => ({
  title: [
    { required: true, message: t('请输入消息标题'), trigger: 'blur' },
    { max: 128, message: t('标题不超过 128 字'), trigger: 'blur' },
  ],
  content: [
    { required: true, message: t('请输入消息正文'), trigger: 'blur' },
    { max: 5000, message: t('正文不超过 5000 字'), trigger: 'blur' },
  ],
  link: [
    {
      pattern: /^\/[^/\s][^\s]*$|^$/,
      message: t('只能填写站内路径，例如 /system/user'),
      trigger: 'blur',
    },
  ],
}))

async function openSend() {
  Object.assign(sendForm, {
    title: '',
    content: '',
    type: 'notice',
    link: '',
    audience: 'all',
    roleIds: [],
    userIds: [],
  })
  sendRef.value?.clearValidate()
  sendVisible.value = true
  if (optionsLoaded.value) return
  try {
    const [r, u] = await Promise.all([roleApi.options(), userApi.options()])
    roles.value = r ?? []
    users.value = u ?? []
    optionsLoaded.value = true
  } catch {
    // 选项加载失败不阻断弹窗，用户可改选全员
  }
}

async function submitSend() {
  const valid = await sendRef.value?.validate().catch(() => false)
  if (!valid) return
  if (sendForm.audience === 'role' && !sendForm.roleIds?.length) {
    ElMessage.warning(t('请选择接收角色'))
    return
  }
  if (sendForm.audience === 'user' && !sendForm.userIds?.length) {
    ElMessage.warning(t('请选择接收用户'))
    return
  }
  sending.value = true
  try {
    const res = await messageApi.send({
      ...sendForm,
      link: sendForm.link?.trim() || undefined,
    })
    ElMessage.success(t('已发送给 {count} 位用户', { count: res.recipients }))
    sendVisible.value = false
    await store.refresh()
    if (canManage.value) {
      tab.value = 'sent'
      reloadSent()
    }
  } finally {
    sending.value = false
  }
}

onMounted(async () => {
  await loadInbox()
  await store.refresh()
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('消息中心') }}</h1>
        <p class="page-desc">{{ $t('站内信、系统通知与公告投递记录，未读消息会同步到顶部铃铛') }}</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" @click="tab === 'inbox' ? loadInbox() : loadSent()">{{ $t('刷新') }}</el-button>
        <el-button v-perm="'system:message:send'" type="primary" :icon="Promotion" @click="openSend">
          {{ $t('发送消息') }}
        </el-button>
      </div>
    </div>

    <div class="card">
      <el-tabs v-model="tab" class="msg-tabs" @tab-change="switchTab">
        <el-tab-pane name="inbox">
          <template #label>
            <span class="tab-label">
              {{ $t('我的消息') }}
              <em v-if="store.unread > 0">{{ store.unread > 99 ? '99+' : store.unread }}</em>
            </span>
          </template>

          <div class="toolbar">
            <div class="toolbar-left">
              <el-checkbox
                :model-value="allChecked"
                :indeterminate="selected.length > 0 && !allChecked"
                :disabled="!items.length"
                @change="(v: any) => toggleAll(Boolean(v))"
              />
              <el-input
                v-model="query.keyword"
                :placeholder="$t('搜索标题或正文')"
                :prefix-icon="Search"
                clearable
                style="width: 220px"
                @keyup.enter="reloadInbox"
                @clear="reloadInbox"
              />
              <el-select v-model="query.status" :placeholder="$t('全部状态')" clearable style="width: 120px" @change="reloadInbox">
                <el-option :label="$t('未读')" value="unread" />
                <el-option :label="$t('已读')" value="read" />
              </el-select>
              <el-select v-model="query.type" :placeholder="$t('全部类型')" clearable style="width: 120px" @change="reloadInbox">
                <el-option v-for="(m, k) in TYPE_META" :key="k" :label="m.label" :value="k" />
              </el-select>
              <el-button @click="reloadInbox">{{ $t('查询') }}</el-button>
              <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
            </div>
            <div class="toolbar-right">
              <template v-if="selected.length">
                <span class="picked">{{ $t('已选 {count} 条', { count: selected.length }) }}</span>
                <el-button :icon="Delete" size="small" @click="removeSelected">{{ $t('批量删除') }}</el-button>
              </template>
              <el-button text :disabled="!store.unread" @click="markAllRead">{{ $t('全部已读') }}</el-button>
              <el-button text @click="clearRead">{{ $t('清空已读') }}</el-button>
            </div>
          </div>

          <div v-loading="loading" class="msg-list">
            <div
              v-for="m in items"
              :key="m.id"
              class="msg-row"
              :class="{ unread: !m.readAt }"
              @click="openDetail(m)"
            >
              <el-checkbox
                :model-value="selected.includes(m.messageId)"
                class="row-check"
                @click.stop
                @change="(v: any) => (v ? selected.push(m.messageId) : (selected = selected.filter((i) => i !== m.messageId)))"
              />
              <span class="unread-dot" />
              <div class="msg-main">
                <div class="msg-top">
                  <span class="msg-title">{{ m.title }}</span>
                  <el-tag :type="typeMeta(m.type).tone" size="small" effect="light">
                    {{ typeMeta(m.type).label }}
                  </el-tag>
                </div>
                <p class="msg-excerpt">{{ m.excerpt }}</p>
                <div class="msg-meta">
                  <span>{{ m.senderName || $t('系统') }}</span>
                  <span class="sep">·</span>
                  <span :title="fmtTime(m.createdAt)">{{ fromNow(m.createdAt) }}</span>
                </div>
              </div>
              <div class="msg-actions">
                <el-button v-if="!m.readAt" link type="primary" @click.stop="markRead(m)">{{ $t('标记已读') }}</el-button>
                <el-button link type="danger" @click.stop="removeOne(m)">{{ $t('删除') }}</el-button>
              </div>
            </div>

            <div v-if="!loading && !items.length" class="empty">
              <p>{{ query.keyword || query.status || query.type ? $t('没有符合条件的消息') : $t('暂无消息') }}</p>
              <span>{{ $t('系统通知、公告与管理员发送的站内信都会出现在这里') }}</span>
            </div>
          </div>

          <div class="pager">
            <span class="total">{{ $t('共 {count} 条消息', { count: total }) }}</span>
            <el-pagination
              v-model:current-page="query.page"
              v-model:page-size="query.page_size"
              :total="total"
              :page-sizes="[20, 50]"
              layout="sizes, prev, pager, next"
              background
              @current-change="loadInbox"
              @size-change="reloadInbox"
            />
          </div>
        </el-tab-pane>

        <el-tab-pane v-if="canManage" name="sent">
          <template #label>
            <span class="tab-label">{{ $t('发送记录') }}</span>
          </template>

          <div class="toolbar">
            <div class="toolbar-left">
              <el-input
                v-model="sentQuery.keyword"
                :placeholder="$t('搜索标题')"
                :prefix-icon="Search"
                clearable
                style="width: 200px"
                @keyup.enter="reloadSent"
                @clear="reloadSent"
              />
              <el-select v-model="sentQuery.type" :placeholder="$t('全部类型')" clearable style="width: 120px" @change="reloadSent">
                <el-option v-for="(m, k) in TYPE_META" :key="k" :label="m.label" :value="k" />
              </el-select>
              <el-select v-model="sentQuery.status" :placeholder="$t('全部状态')" clearable style="width: 120px" @change="reloadSent">
                <el-option :label="$t('已发送')" value="sent" />
                <el-option :label="$t('已撤回')" value="revoked" />
              </el-select>
              <el-button @click="reloadSent">{{ $t('查询') }}</el-button>
            </div>
            <div class="toolbar-right">
              <el-button :icon="Refresh" circle :title="$t('刷新')" @click="loadSent" />
            </div>
          </div>

          <el-table v-loading="sentLoading" :data="sentRows" row-key="id">
            <el-table-column :label="$t('消息')" min-width="220">
              <template #default="{ row }">
                <div class="sent-title">{{ row.title }}</div>
                <div class="sent-sub">
                  <span>{{ row.senderName || $t('系统') }}</span>
                  <span class="sep">·</span>
                  <span>{{ audienceLabel(row.audience) }}</span>
                </div>
              </template>
            </el-table-column>

            <el-table-column :label="$t('类型')" width="88">
              <template #default="{ row }">
                <el-tag :type="typeMeta(row.type).tone" size="small" effect="light">
                  {{ typeMeta(row.type).label }}
                </el-tag>
              </template>
            </el-table-column>

            <el-table-column :label="$t('送达 / 已读')" width="164">
              <template #default="{ row }">
                <div class="read-line">
                  <span class="num">{{ row.read }}</span>
                  <span class="dim">/ {{ row.total }}</span>
                  <span class="pct num">· {{ readRate(row) }}%</span>
                </div>
                <div class="read-bar"><i :style="{ width: `${readRate(row)}%` }" /></div>
              </template>
            </el-table-column>

            <el-table-column :label="$t('时间')" width="150">
              <template #default="{ row }">
                <div class="cell-time" :title="fmtTime(row.createdAt)">{{ fromNow(row.createdAt) }}</div>
                <div class="cell-sub num">{{ fmtTime(row.createdAt, false) }}</div>
              </template>
            </el-table-column>

            <el-table-column :label="$t('状态')" width="86">
              <template #default="{ row }">
                <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
                  {{ row.status === 1 ? $t('已发送') : $t('已撤回') }}
                </span>
              </template>
            </el-table-column>

            <el-table-column label="" width="80" align="right" fixed="right">
              <template #default="{ row }">
                <el-button
                  v-if="row.status === 1"
                  v-perm="'system:message:delete'"
                  link
                  type="danger"
                  @click="revoke(row)"
                >
                  {{ $t('撤回') }}
                </el-button>
              </template>
            </el-table-column>

            <template #empty>
              <div class="table-empty">
                <p>{{ $t('还没有发送过消息') }}</p>
                <span>{{ $t('站内信支持全员、按角色或指定用户投递，并跟踪已读情况') }}</span>
              </div>
            </template>
          </el-table>

          <div class="pager">
            <span class="total">{{ $t('共 {count} 条发送记录', { count: sentTotal }) }}</span>
            <el-pagination
              v-model:current-page="sentQuery.page"
              v-model:page-size="sentQuery.page_size"
              :total="sentTotal"
              :page-sizes="[10, 20, 50]"
              layout="sizes, prev, pager, next"
              background
              @current-change="loadSent"
              @size-change="reloadSent"
            />
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 详情抽屉 -->
    <el-drawer v-model="detailVisible" size="520px" :with-header="false" append-to-body>
      <div v-loading="detailLoading" class="detail">
        <template v-if="detail">
          <div class="detail-head">
            <div class="detail-tags">
              <el-tag :type="typeMeta(detail.type).tone" size="small" effect="light">
                {{ typeMeta(detail.type).label }}
              </el-tag>
              <span class="detail-time">{{ fmtTime(detail.createdAt) }}</span>
            </div>
            <h2>{{ detail.title }}</h2>
            <p class="detail-from">{{ $t('来自 {name}', { name: detail.senderName || $t('系统') }) }}</p>
          </div>

          <div class="detail-body">{{ detail.content }}</div>

          <div class="detail-foot">
            <el-button v-if="detail.link" type="primary" plain @click="$router.push(detail.link!)">
              {{ $t('前往查看') }}
            </el-button>
            <el-button @click="detailVisible = false">{{ $t('关闭') }}</el-button>
          </div>
        </template>
      </div>
    </el-drawer>

    <!-- 发送消息 -->
    <el-dialog v-model="sendVisible" :title="$t('发送消息')" width="620px" append-to-body>
      <el-form ref="sendRef" :model="sendForm" :rules="sendRules" label-width="86px">
        <el-form-item :label="$t('接收范围')" prop="audience">
          <el-radio-group v-model="sendForm.audience">
            <el-radio-button value="all">{{ $t('全员') }}</el-radio-button>
            <el-radio-button value="role">{{ $t('按角色') }}</el-radio-button>
            <el-radio-button value="user">{{ $t('指定用户') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="sendForm.audience === 'role'" :label="$t('接收角色')">
          <el-select v-model="sendForm.roleIds" multiple filterable :placeholder="$t('选择一个或多个角色')" style="width: 100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>

        <el-form-item v-if="sendForm.audience === 'user'" :label="$t('接收用户')">
          <el-select
            v-model="sendForm.userIds"
            multiple
            filterable
            :placeholder="$t('按昵称或账号搜索')"
            style="width: 100%"
          >
            <el-option
              v-for="u in users"
              :key="u.id"
              :label="$t('{name}（{username}）', { name: u.nickname || u.username, username: u.username })"
              :value="u.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="$t('标题')" prop="title">
          <el-input v-model="sendForm.title" maxlength="128" show-word-limit :placeholder="$t('一句话说明消息内容')" />
        </el-form-item>

        <el-form-item :label="$t('类型')" prop="type">
          <el-select v-model="sendForm.type" style="width: 180px">
            <el-option v-for="(m, k) in TYPE_META" :key="k" :label="m.label" :value="k" />
          </el-select>
        </el-form-item>

        <el-form-item :label="$t('跳转链接')" prop="link">
          <el-input v-model="sendForm.link" :placeholder="$t('可选，站内路径，如 /system/user')" />
        </el-form-item>

        <el-form-item :label="$t('正文')" prop="content">
          <el-input
            v-model="sendForm.content"
            type="textarea"
            :rows="7"
            maxlength="5000"
            show-word-limit
            resize="vertical"
            :placeholder="$t('消息正文，支持换行')"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <span class="send-hint">{{ $t('发送后立即出现在接收人的消息中心') }}</span>
        <el-button @click="sendVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="sending" @click="submitSend">{{ $t('发送') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.msg-tabs {
  :deep(.el-tabs__header) {
    margin: 0;
    padding: 0 18px;
    border-bottom: 1px solid var(--line-soft);
  }

  :deep(.el-tabs__nav-wrap::after) {
    display: none;
  }

  :deep(.el-tabs__item) {
    height: 46px;
    font-size: 13px;
  }
}

.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;

  em {
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    border-radius: 8px;
    background: #eef2ff;
    color: var(--brand-600);
    font-size: 10px;
    font-style: normal;
    font-weight: 600;
    line-height: 16px;
    text-align: center;
  }
}

.picked {
  font-size: 12px;
  color: var(--ink-500);
}

/* ------------------------------------------------------------- 消息列表 */
.msg-list {
  min-height: 180px;
}

.msg-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 15px 18px;
  border-bottom: 1px solid var(--line-soft);
  cursor: pointer;
  transition: background 0.12s ease;

  &:hover {
    background: var(--ink-50, #fafafa);
  }

  &:hover .msg-actions {
    opacity: 1;
  }
}

.row-check {
  margin-top: 1px;
  flex-shrink: 0;
}

.unread-dot {
  width: 6px;
  height: 6px;
  margin-top: 7px;
  border-radius: 50%;
  background: transparent;
  flex-shrink: 0;
}

.msg-row.unread .unread-dot {
  background: var(--brand-600);
}

.msg-main {
  flex: 1;
  min-width: 0;
}

.msg-top {
  display: flex;
  align-items: center;
  gap: 8px;
}

.msg-title {
  font-size: 13.5px;
  color: var(--ink-700);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg-row.unread .msg-title {
  color: var(--ink-900);
  font-weight: 600;
}

.msg-excerpt {
  margin-top: 4px;
  font-size: 12.5px;
  line-height: 1.55;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg-meta {
  margin-top: 6px;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--ink-300);

  .sep {
    color: var(--ink-200);
  }
}

.msg-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  opacity: 0.45;
  transition: opacity 0.12s ease;
  flex-shrink: 0;
}

.empty {
  padding: 56px 0;
  text-align: center;

  p {
    font-size: 13px;
    color: var(--ink-600);
  }

  span {
    display: block;
    margin-top: 5px;
    font-size: 11.5px;
    color: var(--ink-400);
  }
}

/* ------------------------------------------------------------- 发送记录 */
.sent-title {
  font-size: 13px;
  color: var(--ink-900);
}

.sent-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 3px;
  font-size: 11.5px;
  color: var(--ink-400);

  .sep {
    color: var(--ink-200);
  }
}

.read-line {
  display: flex;
  align-items: baseline;
  gap: 4px;
  font-size: 12px;
  color: var(--ink-700);

  .pct {
    color: var(--ink-400);
  }
}

.read-bar {
  height: 3px;
  margin-top: 5px;
  border-radius: 2px;
  background: var(--ink-100, #eceef1);
  overflow: hidden;

  i {
    display: block;
    height: 100%;
    border-radius: 2px;
    background: var(--brand-600);
  }
}

.cell-time {
  font-size: 12.5px;
  color: var(--ink-700);
}

.cell-sub {
  font-size: 11px;
  color: var(--ink-400);
}

.table-empty {
  padding: 40px 0;
  color: var(--ink-400);

  p {
    font-size: 13px;
    color: var(--ink-600);
    margin-bottom: 4px;
  }

  span {
    font-size: 11.5px;
  }
}

.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-top: 1px solid var(--line-soft);
}

.total {
  font-size: 12.5px;
  color: var(--ink-500);
}

/* --------------------------------------------------------------- 详情 */
.detail {
  padding: 26px 28px;
  min-height: 100%;
  display: flex;
  flex-direction: column;
}

.detail-head {
  .detail-tags {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  h2 {
    margin-top: 12px;
    font-size: 19px;
    font-weight: 650;
    line-height: 1.4;
    color: var(--ink-900);
  }
}

.detail-time {
  font-size: 11.5px;
  color: var(--ink-400);
}

.detail-from {
  margin-top: 6px;
  font-size: 12px;
  color: var(--ink-400);
}

.detail-body {
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid var(--line-soft);
  font-size: 13.5px;
  line-height: 1.85;
  color: var(--ink-700);
  white-space: pre-wrap;
  word-break: break-word;
}

.detail-foot {
  display: flex;
  gap: 8px;
  padding-top: 18px;
}

.send-hint {
  float: left;
  font-size: 11.5px;
  color: var(--ink-400);
  line-height: 32px;
}
</style>
