const ERROR_MESSAGES = {
  invalid_token: 'Authentication failed. Please sign in again.',
  unauthorized: 'Authentication failed. Please sign in again.',
  validation_error: 'Please check the request and try again.',
  rate_limit: 'Too many requests. Please try again later.',
  upstream_error: 'The upstream service failed. Please try again later.',
  internal_error: 'Request failed. Please try again later.',
};

export function normalizeErrorPayload(payload = {}, response = {}) {
  const body = payload && typeof payload === 'object' ? payload : {};
  const error =
    body.error && typeof body.error === 'object' ? body.error : body;
  const code = String(error.code || response.status || 'unknown');
  const requestId =
    error.request_id ||
    error.requestId ||
    body.request_id ||
    body.requestId ||
    response.headers?.get?.('X-Request-Id') ||
    '';
  return {
    code,
    requestId,
    message:
      ERROR_MESSAGES[code] ||
      error.message ||
      response.statusText ||
      'Request failed. Please try again later.',
    raw: body,
  };
}

export function createTranslatedError(payload, response) {
  const normalized = normalizeErrorPayload(payload, response);
  return Object.assign(new Error(normalized.message), {
    ...normalized,
    response,
    payload,
  });
}
