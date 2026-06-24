export const DEFAULT_CHAT_STORAGE_LIMITS = {
  maxSessions: 50,
  maxMessagesPerSession: 200,
  maxBytes: 2 * 1024 * 1024,
};

function byteLength(value) {
  return new TextEncoder().encode(JSON.stringify(value)).length;
}

export function pruneChatSessions(
  sessions = [],
  limits = DEFAULT_CHAT_STORAGE_LIMITS,
) {
  const options = { ...DEFAULT_CHAT_STORAGE_LIMITS, ...limits };
  const sorted = [...sessions]
    .map((session) => ({
      ...session,
      messages: Array.isArray(session.messages)
        ? session.messages.slice(-options.maxMessagesPerSession)
        : [],
    }))
    .sort(
      (left, right) =>
        Number(right.updatedAt || 0) - Number(left.updatedAt || 0),
    )
    .slice(0, options.maxSessions);

  const kept = [];
  for (const session of sorted) {
    const candidate = [...kept, session];
    if (kept.length > 0 && byteLength(candidate) > options.maxBytes) break;
    if (byteLength([session]) > options.maxBytes) continue;
    kept.push(session);
  }
  return kept;
}
