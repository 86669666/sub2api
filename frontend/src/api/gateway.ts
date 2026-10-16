import { buildGatewayUrl, gatewayPath } from './url'

interface GatewayOptions {
  method?: 'GET' | 'POST' | 'DELETE'
  body?: string
  idempotencyKey?: string
  signal?: AbortSignal
}

// Never use the panel Axios client or copy its refresh/JWT/cookie settings.
export function gatewayFetch(path: string, apiKey: string, options: GatewayOptions = {}): Promise<Response> {
  if (!['GET', 'POST', 'DELETE'].includes(options.method ?? 'GET')) throw new Error('Unsupported browser gateway method')
  const headers: Record<string, string> = { Authorization: `Bearer ${apiKey}` }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (options.idempotencyKey !== undefined) headers['Idempotency-Key'] = options.idempotencyKey
  return fetch(buildGatewayUrl(gatewayPath(path)), {
    method: options.method ?? 'GET', headers, body: options.body, signal: options.signal,
    credentials: 'omit', mode: 'cors', redirect: 'error', cache: 'no-store',
  })
}
