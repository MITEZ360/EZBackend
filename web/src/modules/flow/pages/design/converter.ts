import type { FlowNode } from '@/api/flow'

/**
 * 有序节点数组 ↔ Vue Flow 图 的无损转换。
 *
 * 后端模型只有有序数组，没有坐标与连线；画布只作为「可视化排序」的外壳：
 * - fromNodes：按数组顺序生成竖排节点与串联边；
 * - toNodes：按 y 坐标排序导出，丢弃边与坐标。
 *
 * 编辑器禁止自由连线和删边，用户只能调整纵向顺序，因此转换双向无损。
 */

export interface CanvasNodeData {
  node: FlowNode
  label: string
  meta: string
}

export const NODE_GAP = 116
export const NODE_X = 120

export function fromNodes(nodes: FlowNode[]): {
  nodes: { id: string; type: string; position: { x: number; y: number }; data: CanvasNodeData }[]
  edges: { id: string; source: string; target: string; type: string; animated: boolean }[]
} {
  const canvas = nodes.map((node, i) => ({
    id: node.id,
    type: 'flowNode',
    position: { x: NODE_X, y: i * NODE_GAP },
    data: { node, label: node.name || '未命名节点', meta: nodeMeta(node) },
  }))
  const edges = canvas.slice(1).map((item, i) => ({
    id: `e-${canvas[i].id}-${item.id}`,
    source: canvas[i].id,
    target: item.id,
    type: 'smoothstep',
    animated: false,
  }))
  return { nodes: canvas, edges }
}

export function toNodes(
  nodes: { id: string; position: { x: number; y: number }; data: CanvasNodeData }[],
): FlowNode[] {
  return [...nodes]
    .sort((a, b) => a.position.y - b.position.y)
    .map((item) => item.data.node)
}

export function nodeMeta(node: FlowNode): string {
  const parts: string[] = []
  if (node.type === 'cc') parts.push('抄送')
  else {
    parts.push(node.mode === 'all' ? '会签' : '或签')
  }
  if (node.sequential) parts.push('顺序')
  if (node.skipWhen) parts.push('条件跳过')
  return parts.join(' · ')
}

export function newNode(type: FlowNode['type'], index: number): FlowNode {
  const id = `n${Date.now().toString(36)}${index}`
  return {
    id,
    name: type === 'cc' ? '抄送节点' : '审批节点',
    type,
    mode: 'all',
    onReject: 'backToInitiator',
    assignees: [],
  }
}
