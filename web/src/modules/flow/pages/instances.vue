<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Search, View } from '@element-plus/icons-vue'
import { flowApi, type FlowDef, type InstanceDetail, type FlowInstance } from '../api'
import type { ListQuery, PageResult } from '@/api/types'
import { fmtTime } from '@/utils/format'

const rows = ref<FlowInstance[]>([])
const total = ref(0)
const loading = ref(false)
const listQuery = reactive({ page: 1, page_size: 10, keyword: '', status: '' })

const flows = ref<FlowDef[]>([])
const startVisible = ref(false)
const starting = ref(false)
const startForm = reactive({ flowKey: '', title: '', variables: '{}' })
const variablesError = ref('')

async function load() {
  loading.value = true
  try {
    const q: ListQuery = { page: listQuery.page, page_size: listQuery.page_size, keyword: listQuery.keyword || undefined, mine: 1 }
    if (listQuery.status) q.status = listQuery.status
    const res: PageResult<FlowInstance> = await flowApi.instances(q)
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally { loading.value = false }
}
function reload() { listQuery.page = 1; load() }
function resetQuery() { listQuery.keyword = ''; listQuery.status = ''; reload() }

async function openStart() {
  const res = await flowApi.flows({ page: 1, page_size: 200 })
  flows.value = (res.list ?? []).filter((f) => f.status === 1)
  startForm.flowKey = ''
  startForm.title = ''
  startForm.variables = '{}'
  variablesError.value = ''
  startVisible.value = true
}

async function submitStart() {
  if (!startForm.flowKey) return ElMessage.warning('请选择流程')
  let variables: Record<string, unknown>
  try {
    variables = JSON.parse(startForm.variables || '{}')
    if (typeof variables !== 'object' || Array.isArray(variables)) throw new Error('not object')
  } catch {
    variablesError.value = '变量需为 JSON 对象'
    return
  }
  variablesError.value = ''
  starting.value = true
  try {
    await flowApi.start({ flowKey: startForm.flowKey, title: startForm.title, variables })
    ElMessage.success('已发起')
    startVisible.value = false
    load()
  } finally { starting.value = false }
}

async function cancel(row: FlowInstance) {
  await ElMessageBox.confirm(`确认撤销「${row.title || row.flowName}」？`, '提示', { type: 'warning' })
  await flowApi.cancelInstance(row.id)
  ElMessage.success('已撤销')
  load()
}

/* 详情抽屉 */
const detailVisible = ref(false)
const detail = ref<InstanceDetail | null>(null)
const rollbackTarget = ref('')
const rollbackReason = ref('')

async function openDetail(id: string) {
  detail.value = await flowApi.instance(id)
  rollbackTarget.value = ''
  rollbackReason.value = ''
  detailVisible.value = true
}

async function doRollback() {
  if (!detail.value || !rollbackTarget.value) return ElMessage.warning('请选择退回节点')
  await flowApi.rollback(detail.value.instance.id, rollbackTarget.value, rollbackReason.value)
  ElMessage.success('已回退')
  detailVisible.value = false
  load()
}

const statusText: Record<string, string> = {
  running: '审批中', approved: '已通过', rejected: '已驳回', cancelled: '已撤销',
}
const statusTag: Record<string, string> = {
  running: 'primary', approved: 'success', rejected: 'danger', cancelled: 'info',
}
const taskStatusText: Record<string, string> = {
  pending: '待处理', approved: '已同意', rejected: '已驳回', transferred: '已移交', cancelled: '已取消',
}
const actionText: Record<string, string> = {
  approve: '同意', reject: '驳回', transfer: '移交', addSign: '加签', removeSign: '减签',
  rollback: '回退', cancel: '撤销', skip: '跳过', cc: '抄送', noAssignee: '自动通过',
  timeoutNotify: '超时提醒', timeout: '超时处理',
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2>我发起的</h2>
        <p>查看发起的审批、流转轨迹，必要时撤销或回退</p>
      </div>
      <el-button type="primary" @click="openStart">发起审批</el-button>
    </div>
    <div class="card">
      <div class="toolbar">
        <el-input v-model="listQuery.keyword" placeholder="搜索标题或流程" clearable
                  prefix-icon="Search" style="width: 240px" @keyup.enter="reload" @clear="reload" />
        <el-select v-model="listQuery.status" placeholder="状态" clearable style="width: 130px" @change="reload">
          <el-option label="审批中" value="running" />
          <el-option label="已通过" value="approved" />
          <el-option label="已驳回" value="rejected" />
          <el-option label="已撤销" value="cancelled" />
        </el-select>
        <el-button :icon="Search" @click="reload">查询</el-button>
        <el-button :icon="Refresh" @click="resetQuery">重置</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="title" label="标题" min-width="180" show-overflow-tooltip />
        <el-table-column prop="flowName" label="流程" min-width="130" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTag[row.status] ?? 'info'" size="small">{{ statusText[row.status] ?? row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="当前节点" min-width="120">
          <template #default="{ row }">{{ row.currentNode || '—' }}</template>
        </el-table-column>
        <el-table-column prop="round" label="轮次" width="70" />
        <el-table-column label="发起时间" min-width="160">
          <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" fixed="right" width="180">
          <template #default="{ row }">
            <el-button link type="primary" :icon="View" @click="openDetail(row.id)">详情</el-button>
            <el-button v-if="row.status === 'running'" link type="danger" @click="cancel(row)">撤销</el-button>
          </template>
        </el-table-column>
        <template #empty><el-empty description="暂无发起的审批" /></template>
      </el-table>

      <div class="pager">
        <el-pagination v-model:current-page="listQuery.page" v-model:page-size="listQuery.page_size"
                       :total="total" :page-sizes="[10, 20, 50]" layout="sizes, prev, pager, next, jumper"
                       background @size-change="load" @current-change="load" />
      </div>
    </div>

    <!-- 发起弹窗 -->
    <el-dialog v-model="startVisible" title="发起审批" width="480px" append-to-body destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="选择流程" required>
          <el-select v-model="startForm.flowKey" placeholder="选择已发布的流程" style="width: 100%">
            <el-option v-for="f in flows" :key="f.key" :label="f.name" :value="f.key" />
          </el-select>
        </el-form-item>
        <el-form-item label="标题"><el-input v-model="startForm.title" placeholder="留空则用流程名称" /></el-form-item>
        <el-form-item label="流程变量（JSON）">
          <el-input v-model="startForm.variables" type="textarea" :rows="4"
                    placeholder='例如 {"amount": 8000}' />
          <div v-if="variablesError" class="err">{{ variablesError }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="startVisible = false">取消</el-button>
        <el-button type="primary" :loading="starting" @click="submitStart">发起</el-button>
      </template>
    </el-dialog>

    <!-- 详情抽屉 -->
    <el-drawer v-model="detailVisible" title="审批详情" size="620px" append-to-body>
      <template v-if="detail">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="标题">{{ detail.instance.title }}</el-descriptions-item>
          <el-descriptions-item label="流程">{{ detail.instance.flowName }}</el-descriptions-item>
          <el-descriptions-item label="状态">{{ statusText[detail.instance.status] }}</el-descriptions-item>
          <el-descriptions-item label="当前节点">{{ detail.instance.currentNode || '—' }}</el-descriptions-item>
        </el-descriptions>

        <h4 class="section-title">流转轨迹</h4>
        <el-timeline>
          <el-timeline-item v-for="r in detail.records" :key="r.id" :timestamp="fmtTime(r.createdAt)" placement="top">
            <span class="record-action">{{ actionText[r.action] ?? r.action }}</span>
            <span class="record-name">{{ r.nodeName }}</span>
            <span v-if="r.comment" class="record-comment">{{ r.comment }}</span>
          </el-timeline-item>
        </el-timeline>

        <h4 class="section-title">任务</h4>
        <el-table :data="detail.tasks" size="small" style="width: 100%">
          <el-table-column prop="nodeName" label="节点" min-width="100" />
          <el-table-column prop="assigneeId" label="处理人" min-width="120" />
          <el-table-column label="状态" width="90">
            <template #default="{ row }">{{ taskStatusText[row.status] ?? row.status }}</template>
          </el-table-column>
          <el-table-column prop="comment" label="意见" min-width="120" show-overflow-tooltip />
        </el-table>

        <template v-if="detail.instance.status === 'running'">
          <h4 class="section-title">回退节点</h4>
          <div class="rollback">
            <el-select v-model="rollbackTarget" placeholder="选择退回的节点" style="width: 220px">
              <el-option v-for="n in detail.instance.nodesSnapshot" :key="n.id" :label="n.name" :value="n.id" />
            </el-select>
            <el-input v-model="rollbackReason" placeholder="原因" style="width: 200px" />
            <el-button type="warning" @click="doRollback">回退</el-button>
          </div>
        </template>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.err { margin-top: 4px; font-size: 12px; color: var(--danger); }
.section-title { margin: 20px 0 10px; font-size: 13px; font-weight: 500; color: var(--ink-800); }
.record-action { font-weight: 500; color: var(--ink-800); }
.record-name { margin-left: 8px; color: var(--ink-600); }
.record-comment { display: block; margin-top: 3px; color: var(--ink-500); font-size: 12px; }
.rollback { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
