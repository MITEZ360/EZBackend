<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { newsCategoryApi } from '@/api/news'
import type { NewsCategory } from '@/api/types'
import IconField from '@/components/IconField.vue'

const rows = ref<NewsCategory[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = await newsCategoryApi.list()
  } finally {
    loading.value = false
  }
}

onMounted(load)

/* ------------------------------------------------------------ 编辑 */
const drawer = ref(false)
const editingId = ref('')
const isEdit = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = ref({
  code: '',
  name: '',
  icon: '',
  color: '',
  sort: 0,
  status: 1,
  remark: '',
})

const rules: FormRules = {
  code: [
    { required: true, message: '请输入编码', trigger: 'blur' },
    {
      pattern: /^[a-z][a-z0-9_-]{1,31}$/,
      message: '小写字母开头，可含小写字母、数字、下划线、连字符',
      trigger: 'blur',
    },
  ],
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
}

function openCreate() {
  editingId.value = ''
  isEdit.value = false
  form.value = { code: '', name: '', icon: '', color: '', sort: (rows.value.length + 1) * 10, status: 1, remark: '' }
  formRef.value?.clearValidate()
  drawer.value = true
}

function openEdit(row: NewsCategory) {
  editingId.value = row.id
  isEdit.value = true
  form.value = {
    code: row.code,
    name: row.name,
    icon: row.icon ?? '',
    color: row.color ?? '',
    sort: row.sort ?? 0,
    status: row.status,
    remark: row.remark ?? '',
  }
  formRef.value?.clearValidate()
  drawer.value = true
}

async function submit() {
  const ok = await formRef.value?.validate().catch(() => false)
  if (!ok) return
  saving.value = true
  try {
    if (isEdit.value) {
      await newsCategoryApi.update(editingId.value, { ...form.value })
      ElMessage.success('已保存')
    } else {
      await newsCategoryApi.create({ ...form.value })
      ElMessage.success('已创建')
    }
    drawer.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function toggle(row: NewsCategory) {
  const next = row.status === 1 ? 0 : 1
  await newsCategoryApi.update(row.id, {
    code: row.code,
    name: row.name,
    icon: row.icon,
    color: row.color,
    sort: row.sort,
    remark: row.remark,
    status: next,
  })
  row.status = next
  ElMessage.success(next === 1 ? '已启用' : '已停用')
}

async function remove(row: NewsCategory) {
  try {
    await ElMessageBox.confirm(
      `删除后爬虫不能再推送该分类。若仍有资讯引用会拒绝删除，可先停用。确认删除「${row.name}」？`,
      '删除分类',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await newsCategoryApi.remove(row.id)
  ElMessage.success('已删除')
  await load()
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">资讯分类</h1>
        <p class="page-desc">
          分类是爬虫推送的白名单：未配置或已停用的分类会被逐条拒收。这里的名称、图标与颜色会随
          <code>/api/open/v1/news/meta</code> 下发，客户端直接用于渲染筛选与标签。
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-button v-perm="'content:news:category'" type="primary" :icon="Plus" @click="openCreate">
          新增分类
        </el-button>
      </div>
    </div>

    <section class="card">
      <el-table :data="rows" v-loading="loading" row-key="id">
        <el-table-column label="分类" min-width="220">
          <template #default="{ row }">
            <div class="cat-cell">
              <div class="cat-icon" :style="{ background: row.color || 'var(--bg-subtle)' }">
                <img v-if="row.icon" :src="row.icon" alt="" />
                <span v-else>{{ row.name.slice(0, 1) }}</span>
              </div>
              <div class="cat-body">
                <div class="cat-name">{{ row.name }}</div>
                <div v-if="row.remark" class="dim cat-remark">{{ row.remark }}</div>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="编码" width="160">
          <template #default="{ row }">
            <span class="mono code">{{ row.code }}</span>
          </template>
        </el-table-column>

        <el-table-column label="资讯数" width="110" align="right">
          <template #default="{ row }">
            <span class="mono">{{ row.articleCount.toLocaleString() }}</span>
          </template>
        </el-table-column>

        <el-table-column prop="sort" label="排序" width="80" align="center" />

        <el-table-column label="状态" width="86">
          <template #default="{ row }">
            <el-switch
              v-perm="'content:news:category'"
              :model-value="row.status"
              :active-value="1"
              :inactive-value="0"
              @change="toggle(row)"
            />
          </template>
        </el-table-column>

        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'content:news:category'" link type="primary" @click="openEdit(row)">
              编辑
            </el-button>
            <el-button v-perm="'content:news:delete'" link type="danger" @click="remove(row)">
              删除
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="empty">
            <p>还没有分类</p>
            <p class="dim">没有分类时，爬虫推送的所有资讯都会被拒收</p>
          </div>
        </template>
      </el-table>
    </section>

    <el-drawer v-model="drawer" :title="isEdit ? '编辑分类' : '新增分类'" size="500px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="76px" label-position="left">
        <el-form-item label="编码" prop="code">
          <el-input v-model="form.code" :disabled="isEdit" placeholder="如：tech" maxlength="32" />
          <p class="field-tip">爬虫推送时按此编码匹配，创建后不可修改</p>
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：科技" maxlength="64" />
        </el-form-item>
        <el-form-item label="图标">
          <IconField
            v-model:icon="form.icon"
            v-model:color="form.color"
            :fallback="form.name"
          />
          <p class="field-tip">建议方形 PNG / SVG，透明底，≤ 512KB</p>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" :max="9999" controls-position="right" />
          <span class="dim switch-tip">数字越小越靠前，客户端按此顺序展示</span>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
          <span class="dim switch-tip">停用后拒绝该分类的写入</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="drawer = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.cat-cell {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.cat-body {
  min-width: 0;
}

.cat-icon {
  width: 34px;
  height: 34px;
  border-radius: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  flex: none;
  color: #fff;
  font-size: 14px;
  font-weight: 600;
}

.cat-icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cat-name {
  font-weight: 500;
  color: var(--ink-800);
}

.cat-remark {
  margin-top: 2px;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.code {
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-subtle);
  color: var(--ink-600);
}

.field-tip {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--ink-400);
  line-height: 1.4;
}

.switch-tip {
  margin-left: 10px;
  font-size: 12px;
}
</style>
