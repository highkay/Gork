import { createTranslatedError } from '../../shared/error-translator.js';

export function buildTokenQuery(params = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    query.set(key, String(value));
  }
  return query.toString();
}

export async function adminFetchJson(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  const key = await window.adminKey?.get?.();
  if (key) headers.Authorization = `Bearer ${key}`;
  const response = await fetch(path, { ...options, headers });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw createTranslatedError(payload, response);
  return payload;
}
