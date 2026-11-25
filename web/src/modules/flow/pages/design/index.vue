<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { VueFlow, useVueFlow, type Connection, type NodeChange, type NodeMouseEvent } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft,
  Delete,
  Plus,
  Refresh,
  Search,
} from '@element-plus/icons-vue'
import {
  flowApi,
  type FlowDef,
  type FlowDefPayload,
  type FlowNode,
} from '../../api'
import type { ListQuery, PageResult } from '@/api/types'
import { fmtTime } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import FlowNodeCard from './FlowNodeCard.vue'
import { fromNodes, newNode, nodeMeta, toNodes } from './converter'

const auth = useAuthStore()
const can = (perm: string) => auth.can(perm)

/* --------------------------------------------------------------- 列表态 */
const rows = ref<FlowDef[]>([])
const total = ref(0)
const loading = ref(false)
const listQuery = reactive({ page: 1, page_size: 10, keyword: '', status: undefined as number | undefined })

async function load() {
  loading.value = true
  try {
    const q: ListQuery = {
      page: listQuery.page,
      page_size: listQuery.page_size,
      keyword: listQuery.keyword || undefined,
    }
    if (listQuery.status !== undefined) q.status = listQuery.status
    const res: PageResult<FlowDef> = await flowApi.flows(q)
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally {
    loading.value = false
  }
}
function reload() { listQuery.page = 1; load() }
function resetQuery() { listQuery.keyword = ''; listQuery.status = undefined; reload() }

/* --------------------------------------------------------------- 设计态 */
const editing = ref(false)
const saving = ref(false)
const flowId = ref('')
const meta = reactive({ key: '', name: '', description: '' })
const nodes = ref<FlowNode[]>([])
const selectedId = ref('')
const drawerVisible = ref(false)

const selected = computed(() => nodes.value.find((n) => n.id === selectedId.value))
const canvas = computed(() => fromNodes(nodes.value))
const { fitView } = useVueFlow({ id: 'flow-designer' })

function resetDesigner() {
  flowId.value = ''
  meta.key = ''
  meta.name = ''
  meta.description = ''
  nodes.value = [baseStartNode()]
  selectedId.value = ''
  drawerVisible.value = false
}

function baseStartNode(): FlowNode {
  return { id: 'start', name: '发起', type: 'approve', mode: 'all', assignees: [] }
}

async function openDesigner(id?: string) {
  resetDesigner()
  if (id) {
    const f = await flowApi.flow(id)
    flowId.value = f.id
    meta.key = f.key
    meta.name = f.name
    meta.description = f.description ?? ''
    nodes.value = f.nodes?.length ? f.nodes.map((n) => ({ ...n })) : [baseStartNode()]
  }
  editing.value = true
  window.setTimeout(() => fitView({ padding: 0.2, duration: 200 }), 60)
}

function backToList() {
  editing.value = false
  load()
}

function addNode(type: FlowNode['type']) {
  const node = newNode(type, nodes.value.length)
  nodes.value.push(node)
  selectedId.value = node.id
  drawerVisible.value = true
}

function removeNode(id: string) {
  if (id === 'start') return ElMessage.warning('发起节点不能删除')
  nodes.value = nodes.value.filter((n) => n.id !== id)
  if (selectedId.value === id) selectedId.value = ''
}

function onNodeClick(event: NodeMouseEvent) {
  selectedId.value = event.node.id
  drawerVisible.value = true
}

function onConnect(_c: Connection) {
  // 固定串联：不允许用户画连线，避免与有序数组模型冲突。
  ElMessage.info('流程为顺序结构，节点顺序请上下拖动调整')
}

const nodePositions = reactive<Record<string, number>>({})

function onNodesChange(changed: NodeChange[]) {
  // Vue Flow 拖动后回写顺序：只取 y，x 固定，保持竖排。
  for (const change of changed) {
    if (change.type === 'position' && change.position) {
      nodePositions[change.id] = change.position.y
    }
  }
  nodes.value = [...nodes.value].sort(
    (a, b) => (nodePositions[a.id] ?? 0) - (nodePositions[b.id] ?? 0),
  )
}

async function save() {
  if (!meta.key.trim() || !meta.name.trim()) return ElMessage.warning('请填写流程标识与名称')
  if (nodes.value.length < 2) return ElMessage.warning('至少配置发起节点和一个审批节点')
  const payload: FlowDefPayload = {
    key: meta.key.trim(),
    name: meta.name.trim(),
    description: meta.description,
    nodes: toNodes(canvas.value.nodes),
  }
  saving.value = true
  try {
    if (flowId.value) await flowApi.updateFlow(flowId.value, payload)
    else await flowApi.createFlow(payload)
    ElMessage.success('保存成功')
    backToList()
  } finally {
    saving.value = false
  }
}

async function toggleStatus(row: FlowDef) {
  const next = row.status === 1 ? 0 : 1
  if (next === 1) {
    for (const n of row.nodes ?? []) {
      if (n.type !== 'cc' && (!n.assignees || n.assignees.length === 0)) {
        return ElMessage.warning(`节点「${n.name}」未配置审批人`)
      }
    }
  }
  await flowApi.setFlowStatus(row.id, next)
  ElMessage.success(next === 1 ? '已发布' : '已转为草稿')
  load()
}

async function removeFlow(row: FlowDef) {
  await ElMessageBox.confirm(`确认删除流程「${row.name}」？`, '提示', { type: 'warning' })
  await flowApi.removeFlow(row.id)
  ElMessage.success('已删除')
  load()
}

/* ------------------------------------------------------- 节点配置抽屉表单 */
const assigneeTypeOptions = [
  { value: 'user', label: '指定用户' },
  { value: 'role', label: '角色' },
  { value: 'deptLeader', label: '部门负责人' },
  { value: 'initiatorLeader', label: '发起人上级' },
  { value: 'initiatorSelf', label: '发起人自己' },
  { value: 'initiatorInput', label: '发起时指定' },
]
const modeOptions = [
  { value: 'all', label: '全部同意（会签）' },
  { value: 'any', label: '任一同意（或签）' },
]
const rejectOptions = [
  { value: 'backToInitiator', label: '退回发起人' },
  { value: 'prevNode', label: '退回上一节点' },
  { value: 'end', label: '直接终止' },
]
const timeoutOptions = [
  { value: 'notify', label: '仅提醒' },
  { value: 'approve', label: '自动通过' },
  { value: 'reject', label: '自动驳回' },
]
const opOptions = [
  { value: 'eq', label: '等于' }, { value: 'ne', label: '不等于' },
  { value: 'gt', label: '大于' }, { value: 'gte', label: '大于等于' },
  { value: 'lt', label: '小于' }, { value: 'lte', label: '小于等于' },
  { value: 'in', label: '属于列表' },
]

function addAssignee() {
  if (!selected.value) return
  selected.value.assignees = selected.value.assignees ?? []
  selected.value.assignees.push({ type: 'user', value: '' })
}
function removeAssignee(i: number) {
  selected.value?.assignees?.splice(i, 1)
}
function toggleSkip() {
  if (!selected.value) return
  if (selected.value.skipWhen) selected.value.skipWhen = null
  else selected.value.skipWhen = { field: '', op: 'lte', value: '' }
}
function metaOf(n: FlowNode) { return nodeMeta(n) }

onMounted(load)
</script>

<template>
  <div class="page">
    <!-- 列表态 -->
    <template v-if="!editing">
      <div class="page-head">
        <div>
          <h2>流程设计</h2>
          <p>配置审批节点与聚合规则，发布后即可发起审批</p>
        </div>
        <el-button v-if="can('flow:def:create')" type="primary" :icon="Plus" @click="openDesigner()">新建流程</el-button>
      </div>
      <div class="card">
        <div class="toolbar">
          <el-input v-model="listQuery.keyword" placeholder="搜索标识或名称" clearable
                    prefix-icon="Search" style="width: 240px" @keyup.enter="reload" @clear="reload" />
          <el-select v-model="listQuery.status" placeholder="状态" clearable style="width: 120px" @change="reload">
            <el-option label="草稿" :value="0" />
            <el-option label="已发布" :value="1" />
          </el-select>
          <el-button :icon="Search" @click="reload">查询</el-button>
          <el-button :icon="Refresh" @click="resetQuery">重置</el-button>
        </div>

        <el-table :data="rows" v-loading="loading" stripe style="width: 100%">
          <el-table-column prop="key" label="标识" min-width="150" />
          <el-table-column prop="name" label="名称" min-width="160" />
          <el-table-column label="节点数" width="90">
            <template #default="{ row }">{{ row.nodes?.length ?? 0 }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
                {{ row.status === 1 ? '已发布' : '草稿' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="version" label="版本" width="80" />
          <el-table-column label="更新时间" min-width="170">
            <template #default="{ row }">{{ fmtTime(row.updatedAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" fixed="right" width="240">
            <template #default="{ row }">
              <el-button v-if="can('flow:def:update')" link type="primary" @click="openDesigner(row.id)">设计</el-button>
              <el-button v-if="can('flow:def:update')" link type="primary"
                         @click="toggleStatus(row)">{{ row.status === 1 ? '停用' : '发布' }}</el-button>
              <el-button v-if="can('flow:def:delete')" link type="danger" @click="removeFlow(row)">删除</el-button>
            </template>
          </el-table-column>
          <template #empty><el-empty description="暂无流程定义" /></template>
        </el-table>

        <div class="pager">
          <el-pagination v-model:current-page="listQuery.page" v-model:page-size="listQuery.page_size"
                         :total="total" :page-sizes="[10, 20, 50]" layout="sizes, prev, pager, next, jumper"
                         background @size-change="load" @current-change="load" />
        </div>
      </div>
    </template>

    <!-- 设计态：三栏 -->
    <template v-else>
      <div class="page-head designer-head">
        <div class="head-left">
          <el-button :icon="ArrowLeft" @click="backToList">返回</el-button>
          <el-input v-model="meta.key" placeholder="流程标识 (key)" style="width: 180px" :disabled="!!flowId" />
          <el-input v-model="meta.name" placeholder="流程名称" style="width: 200px" />
          <el-input v-model="meta.description" placeholder="说明" style="width: 200px" />
        </div>
        <div>
          <el-button :icon="Delete" @click="resetDesigner">重置</el-button>
          <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        </div>
      </div>

      <div class="designer">
        <aside class="palette">
          <div class="palette-title">节点类型</div>
          <button class="palette-item" @click="addNode('approve')">
            <span class="dot brand" />审批节点
          </button>
          <button class="palette-item" @click="addNode('cc')">
            <span class="dot ok" />抄送节点
          </button>
          <p class="palette-tip">点击添加节点，拖动节点调整顺序。流程为顺序结构，不支持自由画连线。</p>
        </aside>

        <section class="canvas-wrap">
          <VueFlow id="flow-designer" :nodes="canvas.nodes" :edges="canvas.edges"
                   :nodes-draggable="true" :nodes-connectable="false" :edges-updatable="false"
                   :delete-key-code="null" :zoom-on-double-click="false" fit-view-on-init
                   @node-click="onNodeClick" @connect="onConnect" @nodes-change="onNodesChange">
            <Background :gap="18" pattern-color="#e4e7ec" />
            <Controls position="bottom-left" />
            <MiniMap position="bottom-right" pannable zoomable />
            <template #node-flowNode="props">
              <FlowNodeCard v-bind="props" />
            </template>
          </VueFlow>
          <div class="node-list">
            <div v-for="(n, i) in nodes" :key="n.id" class="node-row" @click="onNodeClick({ node: { id: n.id } } as never)">
              <span class="node-no">{{ i + 1 }}</span>
              <span class="node-label">{{ n.name }}</span>
              <span class="node-tags">{{ metaOf(n) }}</span>
              <el-button v-if="n.id !== 'start'" link type="danger" :icon="Delete" @click.stop="removeNode(n.id)" />
            </div>
          </div>
        </section>

        <el-drawer v-model="drawerVisible" :with-header="false" size="340px" direction="rtl"
                   append-to-body class="config-drawer">
          <div v-if="selected" class="config">
            <div class="config-head">
              <h3>节点配置</h3>
              <span class="config-id">{{ selected.id }}</span>
            </div>
            <el-form label-position="top" size="small">
              <el-form-item label="节点名称">
                <el-input v-model="selected.name" :disabled="selected.id === 'start'" />
              </el-form-item>
              <template v-if="selected.type !== 'cc'">
                <el-form-item label="聚合规则">
                  <el-select v-model="selected.mode" style="width: 100%">
                    <el-option v-for="o in modeOptions" :key="o.value" :label="o.label" :value="o.value" />
                  </el-select>
                </el-form-item>
                <el-form-item label="审批人来源">
                  <div v-for="(a, i) in selected.assignees ?? []" :key="i" class="assignee-row">
                    <el-select v-model="a.type" style="width: 130px">
                      <el-option v-for="o in assigneeTypeOptions" :key="o.value" :label="o.label" :value="o.value" />
                    </el-select>
                    <el-input v-if="a.type !== 'initiatorSelf' && a.type !== 'initiatorLeader' && a.type !== 'deptLeader'"
                              v-model="a.value" placeholder="用户ID / 角色 / 变量名" />
                    <el-button link type="danger" :icon="Delete" @click="removeAssignee(i)" />
                  </div>
                  <el-button link type="primary" :icon="Plus" @click="addAssignee">添加来源</el-button>
                </el-form-item>
                <el-form-item label="驳回策略">
                  <el-select v-model="selected.onReject" style="width: 100%">
                    <el-option v-for="o in rejectOptions" :key="o.value" :label="o.label" :value="o.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-checkbox v-model="selected.sequential">顺序会签（逐个审批）</el-checkbox>
                </el-form-item>
                <el-form-item label="超时时间（小时，0 表示不启用）">
                  <el-input-number v-model="selected.timeoutHours" :min="0" :max="720" style="width: 100%" />
                </el-form-item>
                <el-form-item v-if="selected.timeoutHours" label="超时动作">
                  <el-select v-model="selected.timeoutAction" style="width: 100%">
                    <el-option v-for="o in timeoutOptions" :key="o.value" :label="o.label" :value="o.value" />
                  </el-select>
                </el-form-item>
                <el-form-item>
                  <el-checkbox :model-value="!!selected.skipWhen" @change="toggleSkip">设置跳过条件</el-checkbox>
                </el-form-item>
                <div v-if="selected.skipWhen" class="condition">
                  <el-input v-model="selected.skipWhen.field" placeholder="变量名，如 amount" />
                  <el-select v-model="selected.skipWhen.op" style="width: 120px">
                    <el-option v-for="o in opOptions" :key="o.value" :label="o.label" :value="o.value" />
                  </el-select>
                  <el-input v-model="selected.skipWhen.value as string" placeholder="值" />
                </div>
              </template>
              <el-alert v-else type="info" :closable="false" title="抄送节点自动完成，不阻塞流程" />
            </el-form>
          </div>
        </el-drawer>
      </div>
    </template>
  </div>
</template>

<style scoped lang="scss">
.designer-head { flex-wrap: wrap; gap: 10px; }
.head-left { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

.designer {
  display: grid;
  grid-template-columns: 168px minmax(0, 1fr);
  gap: 12px;
  height: calc(100vh - 176px);
}

.palette {
  padding: 14px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
}
.palette-title { margin-bottom: 10px; font-size: 13px; font-weight: 500; color: var(--ink-800); }
.palette-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 10px;
  margin-bottom: 6px;
  font-size: 13px;
  color: var(--ink-700);
  text-align: left;
  cursor: pointer;
  background: var(--bg-subtle);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  &:hover { border-color: var(--brand-400); }
}
.dot { width: 8px; height: 8px; border-radius: 50%; }
.dot.brand { background: var(--brand-500); }
.dot.ok { background: var(--ok); }
.palette-tip { margin-top: 14px; font-size: 11px; line-height: 1.6; color: var(--ink-500); }

.canvas-wrap {
  position: relative;
  overflow: hidden;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
}
.canvas-wrap :deep(.vue-flow) { width: 100%; height: 100%; }

.node-list {
  position: absolute;
  top: 12px;
  left: 12px;
  z-index: 5;
  width: 210px;
  max-height: calc(100% - 24px);
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  box-shadow: var(--sh-sm);
}
.node-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  font-size: 12px;
  cursor: pointer;
  border-bottom: 1px solid var(--line-soft);
  &:hover { background: var(--brand-50); }
}
.node-no { color: var(--ink-400); min-width: 16px; }
.node-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--ink-800); }
.node-tags { color: var(--ink-500); font-size: 11px; }

.config { padding: 20px; }
.config-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.config-head h3 { margin: 0; font-size: 14px; font-weight: 500; }
.config-id { font-family: var(--font-mono); font-size: 11px; color: var(--ink-500); }
.assignee-row { display: flex; align-items: center; gap: 6px; width: 100%; margin-bottom: 6px; }
.condition { display: flex; flex-direction: column; gap: 6px; width: 100%; }
</style>
