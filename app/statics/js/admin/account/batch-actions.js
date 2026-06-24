export function selectedIds(rows = []) {
  return rows.filter((row) => row.selected).map((row) => row.id);
}

export function canRunBatch(rows = []) {
  return selectedIds(rows).length > 0;
}
