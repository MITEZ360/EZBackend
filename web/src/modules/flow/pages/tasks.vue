<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { flowApi, type FlowTask, type TaskDetail } from '../api'
import type { ListQuery, PageResult } from '@/api/types'
import { fmtTime } from '@/utils/format'

const tab = ref('todo')
const rows = ref<FlowTask[]>([])
const total = ref(0)
const loading = ref(false)
const listQuery = reactive({ page: 1, page_size: 10, keyword: '' })

async function load() {
  loading.value = true
  try {
    const q: ListQuery = {
      page: listQuery.page, page_size: listQuery.page_size,
      keyword: listQuery.keyword || undefined, status: tab.value,
    }
    const res: PageResult<FlowTask> = await flowApi.tasks(q)
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally { loading.value = false }
}
function reload() { listQuery.page = 1; load() }
function resetQuery() { listQuery.keyword = ''; reload() }
function onTabChange() { reload() }

/* 审批弹窗 */
const dialogVisible = ref(false)
const detail = ref<TaskDetail | null>(null)
const action = ref('approve')
const comment = ref('')
const targetId = ref('')
const submitting = ref(false)

async function openTask(id: string, act: string) {
  detail.value = await flowApi.task(id)
  action.value = act
  comment.value = ''
  targetId.value = ''
  dialogVisible.value = true
}

const actionTitles: Record<string, string> = {
  approve: '同意审批', reject: '驳回审批', transfer: '移交任务',
  addSign: '加签', removeSign: '减签',
}

async function submit() {
  if (!detail.value) return
  const id = detail.value.task.id
  submitting.value = true
  try {
    if (action.value === 'approve') await flowApi.approve(id, comment.value)
    else if (action.value === 'reject') await flowApi.reject(id, comment.value)
    else if (action.value === 'transfer') {
      if (!targetId.value) { ElMessage.warning('请输入目标用户 ID'); return }
      await flowApi.transfer(id, targetId.value, comment.value)
    } else if (action.value === 'addSign') {
      if (!targetId.value) { ElMessage.warning('请输入审批人 ID'); return }
      await flowApi.addSign(id, targetId.value)
    } else await flowApi.removeSign(id, comment.value)
    ElMessage.success('已处理')
    dialogVisible.value = false
    load()
  } finally { submitting.value = false }
}

function detailTitle(row: FlowTask) { return row.instanceId.slice(-8) }

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2>审批任务</h2>
        <p>处理待办、查看已办，支持驳回、移交与加减签</p>
      </div>
    </div>
    <div class="card">
      <el-tabs v-model="tab" @tab-change="onTabChange">
        <el-tab-pane label="待办" name="todo" />
        <el-tab-pane label="已办" name="done" />
      </el-tabs>
      <div class="toolbar">
        <el-input v-model="listQuery.keyword" placeholder="搜索节点名称" clearable
                  prefix-icon="Search" style="width: 220px" @keyup.enter="reload" @clear="reload" />
        <el-button :icon="Search" @click="reload">查询</el-button>
        <el-button :icon="Refresh" @click="resetQuery">重置</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="nodeName" label="节点" min-width="130" />
        <el-table-column label="流程实例" min-width="180">
          <template #default="{ row }">
            <span>{{ detailTitle(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'pending' ? 'primary' : 'info'" size="small">
              {{ row.status === 'pending' ? '待处理' : row.status === 'approved' ? '已同意' : row.status === 'rejected' ? '已驳回' : row.status === 'transferred' ? '已移交' : '已取消' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="comment" label="意见" min-width="140" show-overflow-tooltip />
        <el-table-column label="创建时间" min-width="160">
          <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" fixed="right" width="260">
          <template #default="{ row }">
            <template v-if="row.status === 'pending'">
              <el-button link type="primary" @click="openTask(row.id, 'approve')">同意</el-button>
              <el-button link type="danger" @click="openTask(row.id, 'reject')">驳回</el-button>
              <el-button link @click="openTask(row.id, 'transfer')">移交</el-button>
              <el-button link @click="openTask(row.id, 'addSign')">加签</el-button>
              <el-button link @click="openTask(row.id, 'removeSign')">减签</el-button>
            </template>
            <span v-else class="finished">已处理</span>
          </template>
        </el-table-column>
        <template #empty><el-empty :description="tab === 'todo' ? '暂无待办' : '暂无已办'" /></template>
      </el-table>

      <div class="pager">
        <el-pagination v-model:current-page="listQuery.page" v-model:page-size="listQuery.page_size"
                       :total="total" :page-sizes="[10, 20, 50]" layout="sizes, prev, pager, next, jumper"
                       background @size-change="load" @current-change="load" />
      </div>
    </div>

    <el-dialog v-model="dialogVisible" :title="actionTitles[action]" width="460px" append-to-body destroy-on-close>
      <template v-if="detail">
        <el-descriptions :column="1" border size="small" style="margin-bottom: 14px">
          <el-descriptions-item label="流程">{{ detail.instance.flowName }}</el-descriptions-item>
          <el-descriptions-item label="标题">{{ detail.instance.title }}</el-descriptions-item>
          <el-descriptions-item label="节点">{{ detail.task.nodeName }}</el-descriptions-item>
        </el-descriptions>
        <el-form label-position="top">
          <el-form-item v-if="action === 'transfer' || action === 'addSign'"
                        :label="action === 'transfer' ? '目标用户 ID' : '审批人用户 ID'" required>
            <el-input v-model="targetId" placeholder="输入用户 ID" />
          </el-form-item>
          <el-form-item v-if="action !== 'addSign'" label="审批意见">
            <el-input v-model="comment" type="textarea" :rows="3" placeholder="可填写意见" />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button :type="action === 'reject' ? 'danger' : 'primary'" :loading="submitting" @click="submit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.finished { color: var(--ink-400); font-size: 12px; }
</style>
