<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { flowApi, type DelegatePayload, type FlowDelegate } from '../api'
import type { ListQuery, PageResult } from '@/api/types'
import { fmtTime } from '@/utils/format'

const rows = ref<FlowDelegate[]>([])
const total = ref(0)
const loading = ref(false)
const listQuery = reactive({ page: 1, page_size: 10, all: false })

async function load() {
  loading.value = true
  try {
    const q: ListQuery = { page: listQuery.page, page_size: listQuery.page_size }
    if (listQuery.all) q.all = 1
    const res: PageResult<FlowDelegate> = await flowApi.delegates(q)
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally { loading.value = false }
}
function reload() { listQuery.page = 1; load() }

const dialogVisible = ref(false)
const editingId = ref('')
const form = reactive<DelegatePayload>({
  fromUserId: '', toUserId: '', startAt: '', endAt: '', enabled: true, remark: '',
})

function toLocalInput(d: Date) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function openCreate() {
  editingId.value = ''
  const now = new Date()
  const end = new Date(now.getTime() + 7 * 24 * 3600 * 1000)
  Object.assign(form, {
    fromUserId: '', toUserId: '', startAt: toLocalInput(now), endAt: toLocalInput(end),
    enabled: true, remark: '',
  })
  dialogVisible.value = true
}

function openEdit(row: FlowDelegate) {
  editingId.value = row.id
  Object.assign(form, {
    fromUserId: row.fromUserId, toUserId: row.toUserId,
    startAt: toLocalInput(new Date(row.startAt)), endAt: toLocalInput(new Date(row.endAt)),
    enabled: row.enabled, remark: row.remark ?? '',
  })
  dialogVisible.value = true
}

async function submit() {
  if (!form.fromUserId || !form.toUserId) return ElMessage.warning('请填写委托人与被委托人')
  const payload: DelegatePayload = {
    ...form,
    startAt: new Date(form.startAt).toISOString(),
    endAt: new Date(form.endAt).toISOString(),
  }
  if (editingId.value) await flowApi.updateDelegate(editingId.value, payload)
  else await flowApi.createDelegate(payload)
  ElMessage.success('已保存')
  dialogVisible.value = false
  load()
}

async function remove(row: FlowDelegate) {
  await ElMessageBox.confirm('确认删除该委托？', '提示', { type: 'warning' })
  await flowApi.removeDelegate(row.id)
  ElMessage.success('已删除')
  load()
}

function statusOf(row: FlowDelegate) {
  if (!row.enabled) return '已停用'
  const now = Date.now()
  if (now < new Date(row.startAt).getTime()) return '未开始'
  if (now > new Date(row.endAt).getTime()) return '已过期'
  return '生效中'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h2>审批委托</h2>
        <p>在指定时段把审批任务委托给他人，解析审批人时自动替换</p>
      </div>
      <el-button type="primary" :icon="Plus" @click="openCreate">新增委托</el-button>
    </div>
    <div class="card">
      <div class="toolbar">
        <el-checkbox v-model="listQuery.all" @change="reload">查看全部</el-checkbox>
        <el-button :icon="Refresh" @click="reload">刷新</el-button>
      </div>
      <el-table :data="rows" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="fromUserId" label="委托人" min-width="130" />
        <el-table-column prop="toUserId" label="被委托人" min-width="130" />
        <el-table-column label="开始时间" min-width="170">
          <template #default="{ row }">{{ fmtTime(row.startAt) }}</template>
        </el-table-column>
        <el-table-column label="结束时间" min-width="170">
          <template #default="{ row }">{{ fmtTime(row.endAt) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusOf(row) === '生效中' ? 'success' : 'info'" size="small">{{ statusOf(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
        <el-table-column label="操作" fixed="right" width="140">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty><el-empty description="暂无委托记录" /></template>
      </el-table>
      <div class="pager">
        <el-pagination v-model:current-page="listQuery.page" v-model:page-size="listQuery.page_size"
                       :total="total" :page-sizes="[10, 20, 50]" layout="sizes, prev, pager, next, jumper"
                       background @size-change="load" @current-change="load" />
      </div>
    </div>

    <el-dialog v-model="dialogVisible" title="审批委托" width="480px" append-to-body destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="委托人用户 ID" required>
          <el-input v-model="form.fromUserId" placeholder="被委托人的用户 ID" />
        </el-form-item>
        <el-form-item label="被委托人用户 ID" required>
          <el-input v-model="form.toUserId" placeholder="接收任务的用户 ID" />
        </el-form-item>
        <el-form-item label="生效开始"><el-input v-model="form.startAt" type="datetime-local" /></el-form-item>
        <el-form-item label="生效结束"><el-input v-model="form.endAt" type="datetime-local" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item>
        <el-form-item><el-checkbox v-model="form.enabled">启用</el-checkbox></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
