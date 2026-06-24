export function setNestedPatch(target, dotted, value) {
  const parts = String(dotted || '')
    .split('.')
    .filter(Boolean);
  let current = target;
  while (parts.length > 1) {
    const part = parts.shift();
    current[part] ||= {};
    current = current[part];
  }
  if (parts.length) current[parts[0]] = value;
  return target;
}
