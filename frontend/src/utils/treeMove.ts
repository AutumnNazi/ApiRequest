import type { NodeSummary } from '../ipc';

function nodeMap(nodes: NodeSummary[]): Map<string, NodeSummary> {
  return new Map(nodes.map((node) => [node.id, node]));
}

function hasSelectedAncestor(id: string, selected: Set<string>, byId: Map<string, NodeSummary>): boolean {
  let parentId = byId.get(id)?.parentId ?? '';
  const seen = new Set<string>();
  while (parentId && !seen.has(parentId)) {
    if (selected.has(parentId)) return true;
    seen.add(parentId);
    parentId = byId.get(parentId)?.parentId ?? '';
  }
  return false;
}

export function orderedTopLevelSelection(nodes: NodeSummary[], ids: string[]): string[] {
  const selected = new Set(ids);
  const byId = nodeMap(nodes);
  const children = new Map<string, NodeSummary[]>();
  for (const node of nodes) {
    const parentId = node.parentId ?? '';
    const siblings = children.get(parentId) ?? [];
    siblings.push(node);
    children.set(parentId, siblings);
  }
  for (const siblings of children.values()) {
    siblings.sort((a, b) => a.sortOrder - b.sortOrder || a.createdAt - b.createdAt);
  }
  const ordered: NodeSummary[] = [];
  const visited = new Set<string>();
  const walk = (parentId: string) => {
    for (const node of children.get(parentId) ?? []) {
      if (visited.has(node.id)) continue;
      visited.add(node.id);
      ordered.push(node);
      walk(node.id);
    }
  };
  walk('');
  for (const node of nodes) {
    if (!visited.has(node.id)) ordered.push(node);
  }
  return ordered
    .filter((node) => selected.has(node.id) && !hasSelectedAncestor(node.id, selected, byId))
    .map((node) => node.id);
}

function isDescendant(byId: Map<string, NodeSummary>, ancestorId: string, candidateId: string): boolean {
  let current = candidateId;
  const seen = new Set<string>();
  while (current && !seen.has(current)) {
    if (current === ancestorId) return true;
    seen.add(current);
    current = byId.get(current)?.parentId ?? '';
  }
  return false;
}

export function canMoveNodesInto(nodes: NodeSummary[], ids: string[], parentId: string): boolean {
  const byId = nodeMap(nodes);
  const parent = byId.get(parentId);
  if (!parent || (parent.kind !== 'collection' && parent.kind !== 'folder')) return false;
  const topLevel = orderedTopLevelSelection(nodes, ids);
  return topLevel.length > 0 && topLevel.every((id) => {
    const node = byId.get(id);
    return !!node && node.kind !== 'collection' && id !== parentId && !isDescendant(byId, id, parentId);
  });
}

export function canMoveNodesToRoot(nodes: NodeSummary[], ids: string[]): boolean {
  const byId = nodeMap(nodes);
  const topLevel = orderedTopLevelSelection(nodes, ids);
  return topLevel.length > 0 && topLevel.every((id) => byId.get(id)?.kind === 'request');
}
