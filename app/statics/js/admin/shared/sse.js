import { createTranslatedError } from '../../shared/error-translator.js';

export async function streamAdminSSE(path, options = {}) {
  const headers = { Accept: 'text/event-stream', ...(options.headers || {}) };
  const key = await window.adminKey?.get?.();
  if (key) headers.Authorization = `Bearer ${key}`;

  const response = await fetch(path, {
    method: 'GET',
    headers,
    signal: options.signal,
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    throw createTranslatedError(payload, response);
  }
  if (!response.body) throw new Error('Streaming response is not readable');

  await consumeSSE(response.body, options.onEvent || (() => {}));
}

export async function consumeSSE(body, onEvent) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      buffer = drainSSEBuffer(buffer, onEvent);
    }
    buffer += decoder.decode();
    drainSSEBuffer(`${buffer}\n\n`, onEvent);
  } finally {
    reader.releaseLock();
  }
}

function drainSSEBuffer(buffer, onEvent) {
  let next = buffer.indexOf('\n\n');
  while (next !== -1) {
    const chunk = buffer.slice(0, next);
    buffer = buffer.slice(next + 2);
    const event = parseSSEChunk(chunk);
    if (event) onEvent(event);
    next = buffer.indexOf('\n\n');
  }
  return buffer;
}

function parseSSEChunk(chunk) {
  const data = chunk
    .split(/\r?\n/)
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trimStart())
    .join('\n')
    .trim();
  if (!data) return null;
  return JSON.parse(data);
}
