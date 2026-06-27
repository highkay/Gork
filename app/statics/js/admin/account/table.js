export function maskToken(value) {
  const text = String(value || '');
  if (text.length <= 10) return text;
  return `${text.slice(0, 6)}...${text.slice(-4)}`;
}

export function escapeAdminCellValue(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'")
    .replace(/"/g, '&quot;');
}

export function tokenRowView(token = {}) {
  return {
    id: token.id || token.token || '',
    pool: token.pool || 'default',
    status: token.status || 'unknown',
    maskedToken: maskToken(token.token || ''),
    model: token.model || token.model_name || '',
    lastUsedAt: token.last_used_at || token.last_used || '',
  };
}

export function paginateTokenRows(tokens = [], options = {}) {
  const pageSize = Math.max(1, Number(options.pageSize || 100));
  const total = tokens.length;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const page = Math.min(pageCount, Math.max(1, Number(options.page || 1)));
  const start = (page - 1) * pageSize;
  return {
    page,
    pageSize,
    pageCount,
    total,
    rows: tokens.slice(start, start + pageSize).map(tokenRowView),
  };
}

export function tokenMobileDetails(row = {}) {
  const view = tokenRowView(row);
  return [
    { key: 'token', label: 'Token', value: view.maskedToken },
    { key: 'pool', label: 'Pool', value: view.pool },
    { key: 'status', label: 'Status', value: view.status },
    { key: 'model', label: 'Model', value: view.model },
    { key: 'lastUsedAt', label: 'Last used', value: view.lastUsedAt },
  ].filter((field) => field.value !== '');
}
