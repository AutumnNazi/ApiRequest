import { describe, expect, it } from 'vitest';
import type { NodeSummary } from '../ipc';
import { canMoveNodesInto, canMoveNodesToRoot, orderedTopLevelSelection } from './treeMove';

const nodes = [
  { id: 'c1', parentId: '', kind: 'collection', sortOrder: 0 },
  { id: 'f1', parentId: 'c1', kind: 'folder', sortOrder: 0 },
  { id: 'r1', parentId: 'f1', kind: 'request', sortOrder: 0 },
  { id: 'r2', parentId: 'c1', kind: 'request', sortOrder: 1 },
  { id: 'c2', parentId: '', kind: 'collection', sortOrder: 1 },
].map((node) => ({
  ...node,
  workspaceId: 'ws1',
  name: node.id,
  createdAt: 0,
  updatedAt: 0,
})) as NodeSummary[];

describe('tree drag selection rules', () => {
  it('drops selected descendants when their ancestor is also selected', () => {
    expect(orderedTopLevelSelection(nodes, ['r1', 'f1', 'r2'])).toEqual(['f1', 'r2']);
  });

  it('rejects collection and cyclic folder moves as one batch', () => {
    expect(canMoveNodesInto(nodes, ['r2', 'c2'], 'c1')).toBe(false);
    expect(canMoveNodesInto(nodes, ['f1'], 'r1')).toBe(false);
  });

  it('allows only requests to move to the root', () => {
    expect(canMoveNodesToRoot(nodes, ['r1', 'r2'])).toBe(true);
    expect(canMoveNodesToRoot(nodes, ['f1'])).toBe(false);
  });
});
