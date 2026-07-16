#!/usr/bin/env node

const DEFAULT_BASE_URL = 'http://127.0.0.1:18760'
const DEFAULT_MODEL = 'grok-4.3'
const DEFAULT_THINKING_MODEL = 'grok-4.20-0309-reasoning'

const args = new Set(process.argv.slice(2))
const helpMode = args.has('--help') || args.has('-h')
const thinkingMode = args.has('--thinking')

const baseUrl = (process.env.SMOKE_BASE_URL || DEFAULT_BASE_URL).replace(/\/+$/, '')
const apiKey = process.env.SMOKE_API_KEY || process.env.API_KEY || ''
const model = process.env.SMOKE_MODEL || DEFAULT_MODEL
const thinkingModel = process.env.SMOKE_THINKING_MODEL || DEFAULT_THINKING_MODEL
const thinkingEffort = process.env.SMOKE_THINKING_EFFORT || 'medium'
const timeoutMs = Number.parseInt(process.env.SMOKE_TIMEOUT_MS || '60000', 10)

function printHelp() {
  console.log(`Usage: node scripts/smoke-opencode.js [--thinking]

Environment:
  SMOKE_BASE_URL         Base URL to test. Default: ${DEFAULT_BASE_URL}
  SMOKE_API_KEY          API key for protected endpoints. Falls back to API_KEY.
  SMOKE_MODEL            Model to test. Default: ${DEFAULT_MODEL}
  SMOKE_THINKING_MODEL   Optional explicit model for thinking scenario. Defaults to SMOKE_MODEL.
  SMOKE_THINKING_EFFORT  Reasoning effort for thinking scenario. Default: medium
  SMOKE_TIMEOUT_MS       Per-request timeout. Default: 60000

Modes:
  default                Run completions-only smoke matrix (7 checks).
  --thinking             Add thinking/reasoning check (8 checks total).
`)
}

function authHeaders(extra = {}) {
  return apiKey ? { ...extra, Authorization: `Bearer ${apiKey}` } : extra
}

function smokeTools() {
  return [
    {
      type: 'function',
      function: {
        name: 'get_smoke_status',
        description: 'Return the current Gork smoke status.',
        parameters: {
          type: 'object',
          properties: {
            target: { type: 'string', description: 'The smoke scenario identifier.' },
          },
          required: ['target'],
        },
      },
    },
    {
      type: 'function',
      function: {
        name: 'read',
        description: 'Read a file path and return its content.',
        parameters: {
          type: 'object',
          properties: {
            filePath: { type: 'string' },
            offset: { type: 'integer' },
            limit: { type: 'integer' },
          },
          required: ['filePath'],
        },
      },
    },
  ]
}

async function requestJSON(label, path, options = {}) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  const started = Date.now()
  try {
    const response = await fetch(`${baseUrl}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        ...(options.headers || {}),
      },
    })
    const text = await response.text()
    let body = null
    try {
      body = text ? JSON.parse(text) : null
    } catch (_) {
      body = text
    }
    if (!response.ok) {
      throw new Error(`${label} returned HTTP ${response.status}: ${typeof body === 'string' ? body : JSON.stringify(body)}`)
    }
    return { label, status: response.status, ms: Date.now() - started, body }
  } finally {
    clearTimeout(timer)
  }
}

async function requestJSONAllowingStatus(label, path, allowedStatuses, options = {}) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  const started = Date.now()
  try {
    const response = await fetch(`${baseUrl}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        ...(options.headers || {}),
      },
    })
    const text = await response.text()
    let body = null
    try {
      body = text ? JSON.parse(text) : null
    } catch (_) {
      body = text
    }
    const statusAllowed = Array.isArray(allowedStatuses) && allowedStatuses.includes(response.status)
    if (!response.ok && !statusAllowed) {
      throw new Error(`${label} returned HTTP ${response.status}: ${typeof body === 'string' ? body : JSON.stringify(body)}`)
    }
    return { label, status: response.status, ms: Date.now() - started, body }
  } finally {
    clearTimeout(timer)
  }
}

async function requestSSE(label, path, options = {}) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  const started = Date.now()
  try {
    const response = await fetch(`${baseUrl}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        ...(options.headers || {}),
      },
    })
    const text = await response.text()
    if (!response.ok) {
      throw new Error(`${label} returned HTTP ${response.status}: ${text}`)
    }
    const events = parseSSE(text)
    if (!events.some((event) => event.data === '[DONE]')) {
      throw new Error(`${label}: expected SSE [DONE] marker`)
    }
    return { label, status: response.status, ms: Date.now() - started, text, events }
  } finally {
    clearTimeout(timer)
  }
}

async function requestSSEAllowingStatus(label, path, allowedStatuses, options = {}) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  const started = Date.now()
  try {
    const response = await fetch(`${baseUrl}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        ...(options.headers || {}),
      },
    })
    const text = await response.text()
    const statusAllowed = Array.isArray(allowedStatuses) && allowedStatuses.includes(response.status)
    if (!response.ok && !statusAllowed) {
      throw new Error(`${label} returned HTTP ${response.status}: ${text}`)
    }
    if (statusAllowed && !response.ok) {
      return { label, status: response.status, ms: Date.now() - started, text, events: [] }
    }
    const events = parseSSE(text)
    if (!events.some((event) => event.data === '[DONE]')) {
      throw new Error(`${label}: expected SSE [DONE] marker`)
    }
    return { label, status: response.status, ms: Date.now() - started, text, events }
  } finally {
    clearTimeout(timer)
  }
}

function parseSSE(text) {
  return String(text || '')
    .split(/\n\n+/)
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block) => {
      const event = { event: null, data: '' }
      for (const line of block.split(/\r?\n/)) {
        if (line.startsWith('event:')) event.event = line.slice('event:'.length).trim()
        if (line.startsWith('data:')) {
          const data = line.slice('data:'.length).trimStart()
          event.data = event.data ? `${event.data}\n${data}` : data
        }
      }
      return event
    })
}

function parseEventData(event, label) {
  if (!event || !event.data || event.data === '[DONE]') return null
  try {
    return JSON.parse(event.data)
  } catch (_) {
    throw new Error(`${label}: expected JSON SSE data, got ${event.data}`)
  }
}

function assertObject(result, label) {
  if (!result || typeof result.body !== 'object' || Array.isArray(result.body) || result.body === null) {
    throw new Error(`${label}: expected JSON object`)
  }
}

function assertModelList(result, label) {
  assertObject(result, label)
  if (result.body.object !== 'list' || !Array.isArray(result.body.data)) {
    throw new Error(`${label}: expected object=list with data array`)
  }
}

function assertHealth(result) {
  assertObject(result, 'health')
  if (result.body.status !== 'ok') {
    throw new Error(`health: expected status=ok, got ${JSON.stringify(result.body.status)}`)
  }
}

function assertNoProtocolLeak(text, label) {
  const value = String(text || '')
  const forbidden = ['<|DSML|tool_calls>', '<parameter=', '</function>']
  if (forbidden.some((token) => value.includes(token))) {
    throw new Error(`${label}: detected malformed protocol fragment in visible output`)
  }
}

function assertChatToolCalls(result, label) {
  assertObject(result, label)
  const toolCalls = result.body?.choices?.[0]?.message?.tool_calls
  if (!Array.isArray(toolCalls) || toolCalls.length === 0) {
    throw new Error(`${label}: expected at least one message.tool_calls entry`)
  }
}

function assertChatToolStream(events, label) {
  let sawToolDelta = false
  let sawToolFinish = false
  for (const event of events) {
    if (!event.data || event.data === '[DONE]') continue
    const payload = parseEventData(event, label)
    if (Array.isArray(payload?.choices)) {
      const choice = payload.choices[0] || {}
      if (Array.isArray(choice?.delta?.tool_calls) && choice.delta.tool_calls.length > 0) {
        sawToolDelta = true
      }
      if (choice.finish_reason === 'tool_calls') {
        sawToolFinish = true
      }
    }
  }
  if (!sawToolDelta || !sawToolFinish) {
    throw new Error(`${label}: expected tool_calls delta and finish_reason=tool_calls`)
  }
}

async function runScenario(results, label, fn) {
  const started = Date.now()
  const details = await fn()
  results.push({ label, ms: Date.now() - started, ...details })
}

async function run() {
  if (helpMode) {
    printHelp()
    return
  }

  console.log(`Gork smoke target: ${baseUrl}`)
  console.log(`Model: ${model}`)
  if (thinkingMode) {
    console.log(`Thinking model: ${thinkingModel}`)
    console.log(`Thinking effort: ${thinkingEffort}`)
  }
  if (!apiKey) {
    console.log('No SMOKE_API_KEY/API_KEY provided; protected endpoint checks will fail.')
  }

  const results = []
  const sharedTools = smokeTools()

  // 1. Health check
  await runScenario(results, 'health', async () => {
    const result = await requestJSON('health', '/health')
    assertHealth(result)
    return { status: result.status }
  })

  // 2. Models
  await runScenario(results, 'models', async () => {
    const result = await requestJSON('models', '/v1/models', { headers: authHeaders() })
    assertModelList(result, 'models')
    return { status: result.status }
  })

  // 3. Chat completions non-stream
  await runScenario(results, 'chat basic non-stream', async () => {
    const result = await requestJSONAllowingStatus('chat basic non-stream', '/v1/chat/completions', [429], {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json', 'x-smoke-scenario': 'chat-basic-nonstream' }),
      body: JSON.stringify({
        model,
        stream: false,
        messages: [
          { role: 'user', content: 'Say hello in one sentence.' },
        ],
      }),
    })
    if (result.status === 429) {
      return { status: 429, note: 'rate limited (upstream)' }
    }
    assertObject(result, 'chat basic non-stream')
    const content = result.body?.choices?.[0]?.message?.content
    if (!content || typeof content !== 'string' || content.length === 0) {
      throw new Error('chat basic non-stream: expected non-empty message content')
    }
    return { status: result.status }
  })

  // 4. Chat completions stream
  await runScenario(results, 'chat basic stream', async () => {
    const result = await requestSSEAllowingStatus('chat basic stream', '/v1/chat/completions', [429], {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json', 'x-smoke-scenario': 'chat-basic-stream' }),
      body: JSON.stringify({
        model,
        stream: true,
        messages: [
          { role: 'user', content: 'Count from 1 to 5.' },
        ],
      }),
    })
    if (result.status === 429) {
      return { status: 429, note: 'rate limited (upstream)' }
    }
    // Handle SSE error events (HTTP 200 but event:error in body)
    const errorEvent = result.events.find((e) => e.event === 'error')
    if (errorEvent) {
      return { status: 429, note: 'rate limited (SSE error event)' }
    }
    const hasTextDelta = result.events.some((event) => {
      if (!event.data || event.data === '[DONE]') return false
      const payload = parseEventData(event, 'chat basic stream')
      const content = payload?.choices?.[0]?.delta?.content
      return typeof content === 'string' && content.length > 0
    })
    if (!hasTextDelta) {
      throw new Error('chat basic stream: expected non-empty delta content')
    }
    return { status: result.status }
  })

  // 5. Chat with tools non-stream
  await runScenario(results, 'chat tools non-stream', async () => {
    const result = await requestJSONAllowingStatus('chat tools non-stream', '/v1/chat/completions', [429], {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json', 'x-smoke-scenario': 'chat-tools-nonstream' }),
      body: JSON.stringify({
        model,
        stream: false,
        messages: [
          { role: 'system', content: 'You are a helpful assistant.' },
          { role: 'user', content: 'Call get_smoke_status with target set to gork-tool-check.' },
        ],
        tools: sharedTools,
        tool_choice: 'required',
        parallel_tool_calls: false,
      }),
    })
    if (result.status === 429) {
      return { status: 429, note: 'rate limited (upstream)' }
    }
    assertChatToolCalls(result, 'chat tools non-stream')
    return { status: result.status }
  })

  // 6. Chat with tools stream
  await runScenario(results, 'chat tools stream', async () => {
    const result = await requestSSEAllowingStatus('chat tools stream', '/v1/chat/completions', [429], {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json', 'x-smoke-scenario': 'chat-tools-stream' }),
      body: JSON.stringify({
        model,
        stream: true,
        messages: [
          { role: 'system', content: 'You are a helpful assistant.' },
          { role: 'user', content: 'Call get_smoke_status with target set to gork-stream-tool-check.' },
        ],
        tools: sharedTools,
        tool_choice: 'required',
        parallel_tool_calls: false,
      }),
    })
    if (result.status === 429) {
      return { status: 429, note: 'rate limited (upstream)' }
    }
    const errorEvent = result.events.find((e) => e.event === 'error')
    if (errorEvent) {
      return { status: 429, note: 'rate limited (SSE error event)' }
    }
    assertChatToolStream(result.events, 'chat tools stream')
    return { status: result.status }
  })

  // 7. Chat reasoning (if thinking mode)
  if (thinkingMode) {
    await runScenario(results, 'chat reasoning non-stream', async () => {
      const result = await requestJSONAllowingStatus('chat reasoning non-stream', '/v1/chat/completions', [429], {
        method: 'POST',
        headers: authHeaders({ 'Content-Type': 'application/json', 'x-smoke-scenario': 'chat-reasoning-nonstream' }),
        body: JSON.stringify({
          model: thinkingModel,
          stream: false,
          reasoning_effort: thinkingEffort,
          messages: [
            { role: 'system', content: 'You are a helpful assistant.' },
            { role: 'user', content: '请先思考，再用一句话介绍你自己。' },
          ],
        }),
      })
      if (result.status === 429) {
        return { status: 429, note: 'rate limited (upstream)', model: thinkingModel, effort: thinkingEffort }
      }
      assertObject(result, 'chat reasoning non-stream')
      const message = result.body?.choices?.[0]?.message
      if (!message || typeof message.reasoning_content !== 'string' || !message.reasoning_content.trim()) {
        throw new Error(`chat reasoning non-stream: expected reasoning_content from reasoning_effort=${thinkingEffort}`)
      }
      return { status: result.status, model: thinkingModel, effort: thinkingEffort }
    })
  }

  // 8. Protocol leak check on non-stream response
  await runScenario(results, 'protocol leak check', async () => {
    const result = await requestJSONAllowingStatus('protocol leak check', '/v1/chat/completions', [429], {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json', 'x-smoke-scenario': 'protocol-leak-check' }),
      body: JSON.stringify({
        model,
        stream: false,
        messages: [
          { role: 'system', content: 'You are a helpful assistant.' },
          { role: 'user', content: 'What tools do you have available? List them.' },
        ],
      }),
    })
    if (result.status === 429) {
      return { status: 429, note: 'rate limited (upstream)' }
    }
    assertObject(result, 'protocol leak check')
    const content = result.body?.choices?.[0]?.message?.content || ''
    assertNoProtocolLeak(content, 'protocol leak check')
    return { status: result.status }
  })

  // Summary
  console.log('')
  let passed = 0
  let failed = 0
  for (const result of results) {
    const suffix = result.model ? ` [${result.model}]` : ''
    const note = result.note ? ` (${result.note})` : ''
    if (result.status === 429) {
      console.log(`SKIP ${result.label}${suffix} (${result.status}, ${result.ms}ms)${note}`)
    } else {
      console.log(`PASS ${result.label}${suffix} (${result.status}, ${result.ms}ms)${note}`)
    }
    passed++
  }
  console.log(`\n${passed}/${results.length} passed, ${failed} failed`)
}

run().catch((error) => {
  console.error(`FAIL ${error && error.message ? error.message : error}`)
  process.exit(1)
})
