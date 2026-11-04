<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, type UploadRequestOptions } from 'element-plus'
import { Delete, Upload } from '@element-plus/icons-vue'
import { fileApi } from '@/api/ops'

/**
 * 图标 + 主色配置。
 *
 * 图标存可访问的图片 URL 而不是图标名：桌面端、移动端、Flutter 都能直接
 * 用 <img> 渲染，不需要各自维护一套图标映射。颜色用于图标缺失时的兜底。
 */
const props = defineProps<{
  icon?: string
  color?: string
  /** 无图标时显示的首字，通常传名称 */
  fallback?: string
}>()

const emit = defineEmits<{
  'update:icon': [value: string]
  'update:color': [value: string]
}>()

const PRESETS = ['#409EFF', '#67C23A', '#E6A23C', '#F56C6C', '#B37FEB', '#909399']
const uploading = ref(false)

const initial = computed(() => (props.fallback || '?').trim().slice(0, 1).toUpperCase())

async function doUpload(options: UploadRequestOptions) {
  const file = options.file
  if (!file.type.startsWith('image/')) {
    ElMessage.error('请选择图片文件')
    return
  }
  if (file.size > 512 * 1024) {
    ElMessage.error('图标建议不超过 512KB')
    return
  }
  uploading.value = true
  try {
    const form = new FormData()
    form.append('file', file)
    const res = await fileApi.upload(form)
    emit('update:icon', res.url)
    ElMessage.success('图标已上传')
  } finally {
    uploading.value = false
  }
}

function clear() {
  emit('update:icon', '')
}
</script>

<template>
  <div class="icon-field">
    <div class="preview" :style="{ background: color || 'var(--brand-600)' }">
      <img v-if="icon" :src="icon" alt="" />
      <span v-else class="initial">{{ initial }}</span>
    </div>

    <div class="ops">
      <div class="row">
        <el-upload :show-file-list="false" accept="image/*" :http-request="doUpload">
          <el-button size="small" :icon="Upload" :loading="uploading">上传图标</el-button>
        </el-upload>
        <el-button v-if="icon" size="small" link type="danger" :icon="Delete" @click="clear">
          移除
        </el-button>
      </div>
      <el-input
        :model-value="icon"
        size="small"
        placeholder="也可直接填写图片地址"
        maxlength="512"
        @update:model-value="emit('update:icon', $event)"
      />
    </div>

    <div class="colors">
      <button
        v-for="c in PRESETS"
        :key="c"
        class="dot"
        :class="{ active: color === c }"
        :style="{ background: c }"
        type="button"
        @click="emit('update:color', c)"
      />
      <el-color-picker
        :model-value="color"
        size="small"
        @update:model-value="emit('update:color', $event ?? '')"
      />
    </div>
  </div>
</template>

<style scoped lang="scss">
.icon-field {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  flex-wrap: wrap;
}

.preview {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  flex: none;
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.06);
}

.preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.initial {
  color: #fff;
  font-size: 20px;
  font-weight: 600;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.18);
}

.ops {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 240px;
  flex: 1;
}

.row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.colors {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
}

.dot {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid transparent;
  cursor: pointer;
  padding: 0;
  transition: transform 0.15s ease;
}

.dot:hover {
  transform: scale(1.12);
}

.dot.active {
  border-color: var(--ink-800);
  box-shadow: 0 0 0 2px #fff inset;
}
</style>
