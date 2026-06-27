import { createTranslatedError } from '../../shared/error-translator.js';

export { streamAdminSSE } from '../shared/sse.js';

export async function adminCacheFetch(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  const key = await window.adminKey?.get?.();
  if (key) headers.Authorization = `Bearer ${key}`;
  const response = await fetch(path, { ...options, headers });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw createTranslatedError(payload, response);
  return payload;
}
