export function getPath(source, key) {
  if (!source || typeof key !== 'string' || key === '') return undefined;
  let current = source;
  for (const part of key.split('.')) {
    if (current == null || typeof current !== 'object') return undefined;
    current = current[part];
  }
  return current;
}

export function flattenKeys(source, prefix = '') {
  const keys = new Set();
  if (!source || typeof source !== 'object' || Array.isArray(source))
    return keys;
  for (const [key, value] of Object.entries(source)) {
    const next = prefix ? `${prefix}.${key}` : key;
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      for (const child of flattenKeys(value, next)) keys.add(child);
    } else {
      keys.add(next);
    }
  }
  return keys;
}

export function mergeTranslations(...parts) {
  const merge = (base, extra) => {
    if (extra == null) return base;
    if (base == null || typeof base !== 'object' || Array.isArray(base))
      return extra;
    const out = Array.isArray(base) ? base.slice() : { ...base };
    for (const [key, value] of Object.entries(extra)) {
      if (
        out[key] &&
        typeof out[key] === 'object' &&
        !Array.isArray(out[key]) &&
        value &&
        typeof value === 'object' &&
        !Array.isArray(value)
      ) {
        out[key] = merge(out[key], value);
      } else {
        out[key] = value;
      }
    }
    return out;
  };
  return parts.reduce((acc, part) => merge(acc, part || {}), {});
}

export function resolveText(key, current = {}, zh = {}, en = {}, params) {
  let value = getPath(current, key);
  if (value === undefined) value = getPath(zh, key);
  if (value === undefined) value = getPath(en, key);
  if (value === undefined) return key;
  value = String(value);
  if (params) {
    for (const [name, replacement] of Object.entries(params)) {
      value = value.replace(
        new RegExp(`\\{${name}\\}`, 'g'),
        String(replacement),
      );
    }
  }
  return value;
}
