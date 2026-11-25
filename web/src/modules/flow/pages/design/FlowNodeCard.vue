<script setup lang="ts">
import { Handle, Position, type NodeProps } from '@vue-flow/core'
import type { CanvasNodeData } from './converter'

defineProps<NodeProps<CanvasNodeData>>()
</script>

<template>
  <div class="flow-node">
    <Handle type="target" :position="Position.Top" class="flow-handle" />
    <div class="node-index">{{ data.node.id }}</div>
    <div class="node-body">
      <div class="node-title">{{ data.label }}</div>
      <div class="node-meta">{{ data.meta }}</div>
    </div>
    <Handle type="source" :position="Position.Bottom" class="flow-handle" />
  </div>
</template>

<style scoped lang="scss">
.flow-node {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 240px;
  min-height: 64px;
  padding: 10px 14px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-sm);
}

.node-index {
  flex: none;
  min-width: 32px;
  max-width: 72px;
  padding: 3px 6px;
  overflow: hidden;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-500);
  text-align: center;
  text-overflow: ellipsis;
  white-space: nowrap;
  background: var(--ink-100);
  border-radius: var(--r-sm);
}

.node-body { min-width: 0; }

.node-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-800);
}

.node-meta {
  margin-top: 3px;
  font-size: 11px;
  color: var(--ink-500);
}

.flow-handle {
  width: 7px;
  height: 7px;
  background: var(--brand-400);
  border: 1px solid var(--bg-card);
}
</style>
