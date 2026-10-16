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
export async function getSetupStatus(): Promise<SetupStatus> {
  const response = await fetch(buildPanelRootUrl('/setup/status'), {
    credentials: 'omit', redirect: 'error', cache: 'no-store',
  })
  if (!response.ok || response.headers.get('Content-Type')?.split(';')[0].trim() !== 'application/json') {
    throw new Error('Setup status is unavailable')
  }
  const result = await response.json()
  if (result?.code !== 0 || typeof result?.data?.needs_setup !== 'boolean' ||
      !['welcome', 'completed'].includes(result?.data?.step)) throw new Error('Invalid setup status')
  return result.data
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
