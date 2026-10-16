const DEFAULT_API_BASE_URL = '/api/v1'
const API_BASE_URL = normalizeAPIBaseURL(import.meta.env?.VITE_API_BASE_URL)

function normalizePath(path: string): string {
  return path.startsWith('/') ? path : `/${path}`
}

function normalizeAPIBaseURL(value: unknown): string {
  const raw = String(value || DEFAULT_API_BASE_URL).trim() || DEFAULT_API_BASE_URL
  const withoutTrailingSlash = raw.replace(/\/+$/, '')
  if (/^[a-z][a-z\d+.-]*:\/\//i.test(withoutTrailingSlash) || withoutTrailingSlash.startsWith('//')) {
    return withoutTrailingSlash
  }
  return normalizePath(withoutTrailingSlash)
}

export function getAPIBaseURL(): string {
  if (isSeparatedDeployment() && import.meta.env.VITE_API_BASE_URL !== '/api/v1') throw new Error('Separated panel API must be /api/v1')
  return API_BASE_URL
}

export function buildApiUrl(path: string): string {
  const base = getAPIBaseURL().replace(/\/+$/, '')
  let suffix = normalizePath(path)
  if (suffix === DEFAULT_API_BASE_URL) {
    suffix = ''
  } else if (suffix.startsWith(`${DEFAULT_API_BASE_URL}/`)) {
    suffix = suffix.slice(DEFAULT_API_BASE_URL.length)
  }
  return `${base}${suffix}`
}

export function buildPanelRootUrl(path: string): string {
  const suffix = safePath(path)
  if (isSeparatedDeployment()) return `${window.location.origin}${suffix}`
  try {
    const origin =
      typeof window === 'undefined'
        ? new URL(getAPIBaseURL()).origin
        : new URL(getAPIBaseURL(), window.location.origin).origin
    return `${origin}${suffix}`
  } catch {
    return suffix
  }
}

export function isSeparatedDeployment(): boolean {
  return import.meta.env.VITE_DEPLOYMENT_MODE === 'separated'
}

export function getDeploymentContract() {
  if (!isSeparatedDeployment()) return null
  if (getAPIBaseURL() !== '/api/v1') throw new Error('Separated panel API must be /api/v1')
  const pair = originPair(import.meta.env.VITE_PANEL_PUBLIC_ORIGIN, import.meta.env.VITE_GATEWAY_BASE_URL)
  if (typeof window !== 'undefined' && window.location.origin !== pair.panel) throw new Error('Panel origin does not match its build contract')
  return { format: 1, mode: 'separated', candidate: import.meta.env.VITE_BUILD_COMMIT, ...pair }
}

export function buildGatewayUrl(path: string): string {
  const suffix = safePath(path)
  const contract = getDeploymentContract()
  if (contract) return `${contract.gateway}${suffix}`
  const explicit = import.meta.env.VITE_GATEWAY_BASE_URL
  return explicit?.trim() ? `${normalizeOrigin(explicit)}${suffix}` : buildPanelRootUrl(suffix)
}

function hasControlCharacters(value: string): boolean {
  return [...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)
}

export function buildAdminWSUrl(override?: string): URL {
  const path = '/api/v1/admin/ops/ws/qps'
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  if (!isSeparatedDeployment()) {
    return override ? new URL(`${protocol}//${override}${path}`) : new URL(buildPanelRootUrl(path).replace(/^http/, 'ws'))
  }
  // The existing override syntax is host[:port], never a URL or a path.
  if (override !== undefined && (hasControlCharacters(override) || /[ /\\@?#]/.test(override) || !override)) throw new Error('Invalid panel WebSocket authority')
  const target = override ? normalizeOrigin(`${window.location.protocol}//${override}`) : window.location.origin
  if (target !== window.location.origin) throw new Error('Admin WebSocket must use the panel origin')
  return new URL(`${target.replace(/^http/, 'ws')}${path}`)
}

// Shared by the browser and the build renderer; no environment or DNS access.
export function normalizeOrigin(value: unknown): string {
  if (typeof value !== 'string' || (hasControlCharacters(value) || value.includes('\\'))) throw new Error('Invalid origin input')
  const raw = value.trim()
  const match = /^(https?):\/\/([^/?#]+)\/?$/i.exec(raw)
  if (!match || /[@$"\';{}*\s]/.test(match[2])) throw new Error('Origin must be HTTP(S) authority only')
  const authority = /^(\[[0-9a-f:]+\]|[^:]+)(?::([0-9]+))?$/i.exec(match[2])
  if (!authority || (authority[2] !== undefined && (Number(authority[2]) < 1 || Number(authority[2]) > 65535))) throw new Error('Invalid origin port')
  const url = new URL(raw)
  const host = url.hostname
  if (host.length > 253) throw new Error('Host is too long')
  if (host.endsWith('.')) throw new Error('Trailing-dot hosts are unsupported')
  if (host.startsWith('[')) {
    if (!/^\[[0-9a-f:]+\]$/i.test(host)) throw new Error('Invalid IPv6 host')
  } else if (!host.split('.').every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(label))) {
    throw new Error('Invalid DNS host')
  }
  // Reject alternate IPv4 spellings (octal, integer, shortened) instead of
  // silently changing the authority selected by the operator.
  const rawHost = match[2].startsWith('[') ? match[2].slice(0, match[2].indexOf(']') + 1) : match[2].split(':')[0]
  if (/^\d+\.\d+\.\d+\.\d+$/.test(host) && rawHost !== host) throw new Error('Noncanonical IPv4')
  if (url.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(host)) throw new Error('HTTP requires an explicit loopback origin')
  if (!/^https?:\/\/[a-z0-9.:[\]-]+$/i.test(url.origin)) throw new Error('Unsafe origin literal')
  return url.origin
}

export function originPair(panel: unknown, gateway: unknown) {
  const p = normalizeOrigin(panel), g = normalizeOrigin(gateway)
  if (p === g || new URL(p).protocol !== new URL(g).protocol) throw new Error('Separated origins must differ and use the same protocol')
  return { panel: p, gateway: g }
}

export function safePath(value: string): string {
  if (typeof value !== 'string' || !value || (hasControlCharacters(value) || /[ \\#]/.test(value)) || /^[a-z][a-z\d+.-]*:/i.test(value) || value.startsWith('//')) throw new Error('Invalid request path')
  const path = value.startsWith('/') ? value : '/' + value
  const pathname = path.split('?')[0]
  // Reject ambiguous path decoding, while leaving query values untouched.
  if (/\/\/|%(?:2e|2f|5c|25)|(?:^|\/)\.{1,2}(?:\/|$)/i.test(pathname)) throw new Error('Ambiguous request path')
  const parsed = new URL(path, 'https://path.invalid')
  if (parsed.pathname !== pathname) throw new Error('Path normalization is not permitted')
  return path
}

export function gatewayPath(value: string): string {
  const path = safePath(value), name = path.split('?')[0]
  if (name !== '/v1/usage' && name !== '/v1/images/batches' && !name.startsWith('/v1/images/batches/')) throw new Error('Unsupported browser gateway family')
  return path
}
