export function setFieldValue(input, value) {
  if (!input) return;
  if (input.type === 'checkbox') input.checked = Boolean(value);
  else input.value = value == null ? '' : String(value);
}
