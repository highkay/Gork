export function statusLabel(status) {
  if (!status) return 'unknown';
  return status.ok === false ? 'error' : 'ok';
}
