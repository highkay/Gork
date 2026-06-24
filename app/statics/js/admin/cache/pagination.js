export function pageWindow(page, pageSize, total) {
  const safePage = Math.max(1, Number(page) || 1);
  const safePageSize = Math.max(1, Number(pageSize) || 50);
  const safeTotal = Math.max(0, Number(total) || 0);
  return {
    offset: (safePage - 1) * safePageSize,
    limit: safePageSize,
    pageCount: Math.max(1, Math.ceil(safeTotal / safePageSize)),
  };
}
