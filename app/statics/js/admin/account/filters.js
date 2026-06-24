export function countActiveFilters(filters = {}) {
  return Object.values(filters).filter((value) => {
    if (Array.isArray(value)) return value.length > 0;
    return value !== undefined && value !== null && value !== '';
  }).length;
}

export function applyTokenFilters(tokens = [], filters = {}) {
  return tokens.filter((token) => {
    if (filters.pool && token.pool !== filters.pool) return false;
    if (filters.status && token.status !== filters.status) return false;
    if (filters.nsfw !== undefined && token.nsfw !== filters.nsfw) return false;
    return true;
  });
}
