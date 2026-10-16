/**
 * Setup API endpoints
 */
import axios from 'axios'
import { buildPanelRootUrl, isSeparatedDeployment } from './url'

// Create a separate client for setup endpoints (not under /api/v1)
const setupClient = axios.create({
  baseURL: buildPanelRootUrl('/').replace(/\/+$/, ''),
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

export interface SetupStatus {
  needs_setup: boolean
  step: string
}

export interface DatabaseConfig {
  host: string
  port: number
  user: string
  password: string
  dbname: string
  sslmode: string
}

export interface RedisConfig {
  host: string
  port: number
  username: string
  password: string
  db: number
  enable_tls: boolean
}

export interface AdminConfig {
  email: string
  password: string
}

export interface ServerConfig {
  host: string
  port: number
  mode: string
}

export interface InstallRequest {
  database: DatabaseConfig
  redis: RedisConfig
  admin: AdminConfig
  server: ServerConfig
}

export interface InstallResponse {
  message: string
  restart: boolean
}

/**
 * Get setup status
 */
export async function getSetupStatus(signal?: AbortSignal): Promise<SetupStatus> {
  signal?.throwIfAborted()
  const controller = new AbortController()
  const cancel = () => controller.abort(signal?.reason)
  let onAbort!: () => void
  const aborted = new Promise<never>((_resolve, reject) => {
    onAbort = () => reject(controller.signal.reason)
    controller.signal.addEventListener('abort', onAbort, { once: true })
  })
  signal?.addEventListener('abort', cancel, { once: true })
  // One deadline includes connection, headers and body consumption. Racing the
  // whole operation also bounds body readers that do not settle after abort.
  const deadline = setTimeout(() => {
    controller.abort(new DOMException('Setup status timed out', 'TimeoutError'))
  }, 30000)
  try {
    return await Promise.race([aborted, (async () => {
      const response = await fetch(buildPanelRootUrl('/setup/status'), {
        credentials: 'omit', redirect: 'error', cache: 'no-store', signal: controller.signal,
      })
      if (!response.ok || response.headers.get('Content-Type')?.split(';')[0].trim() !== 'application/json') {
        throw new Error('Setup status is unavailable')
      }
      const result = await response.json()
      if (result?.code !== 0 || typeof result?.data?.needs_setup !== 'boolean' ||
          !['welcome', 'completed'].includes(result?.data?.step)) throw new Error('Invalid setup status')
      return result.data
    })()])
  } finally {
    clearTimeout(deadline)
    signal?.removeEventListener('abort', cancel)
    controller.signal.removeEventListener('abort', onAbort)
  }
}

/**
 * Test database connection
 */
export async function testDatabase(config: DatabaseConfig): Promise<void> {
  if (isSeparatedDeployment()) throw new Error('Setup wizard is disabled in separated deployments')
  await setupClient.post('/setup/test-db', config)
}

/**
 * Test Redis connection
 */
export async function testRedis(config: RedisConfig): Promise<void> {
  if (isSeparatedDeployment()) throw new Error('Setup wizard is disabled in separated deployments')
  await setupClient.post('/setup/test-redis', config)
}

/**
 * Perform installation
 */
export async function install(config: InstallRequest): Promise<InstallResponse> {
  if (isSeparatedDeployment()) throw new Error('Setup wizard is disabled in separated deployments')
  const response = await setupClient.post('/setup/install', config)
  return response.data.data
}
