export function parseSseEvents(chunk, previousBuffer = '') {
  const input = `${previousBuffer || ''}${chunk || ''}`;
  const parts = input.split(/\n\n/);
  const buffer = parts.pop() || '';
  const events = [];
  for (const part of parts) {
    const dataLines = part
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trim());
    if (!dataLines.length) continue;
    const data = dataLines.join('\n');
    if (!data || data === '[DONE]') continue;
    try {
      events.push(JSON.parse(data));
    } catch {
      // Ignore malformed stream fragments.
    }
  }
  return { events, buffer };
}
