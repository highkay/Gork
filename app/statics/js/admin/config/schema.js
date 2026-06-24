export function flattenConfigSchema(schema = {}, prefix = '') {
  const rows = [];
  for (const [key, value] of Object.entries(schema)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (value && typeof value === 'object' && !Array.isArray(value))
      rows.push(...flattenConfigSchema(value, path));
    else rows.push({ path, value });
  }
  return rows;
}
