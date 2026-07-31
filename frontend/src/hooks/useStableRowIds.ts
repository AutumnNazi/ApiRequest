import { useId, useRef } from 'react';

// 可编辑表格的 DOM identity 必须独立于数组索引和可变字段值。
export function useStableRowIds(rowCount: number) {
  const tableId = useId();
  const nextId = useRef(0);
  const ids = useRef<string[]>([]);
  const createId = () => `${tableId}-row-${nextId.current++}`;

  while (ids.current.length < rowCount) ids.current.push(createId());
  if (ids.current.length > rowCount) ids.current.length = rowCount;

  return {
    rowIds: ids.current,
    promoteGhostRow() {
      ids.current.push(createId());
    },
    removeRow(index: number) {
      ids.current.splice(index, 1);
    },
  };
}
