export function normalizeCacheItem(item = {}) {
  return {
    name: item.name || item.id || '',
    sizeBytes: Number(item.size_bytes || item.sizeBytes || 0),
    updatedAt: item.updated_at || item.updatedAt || '',
  };
}
