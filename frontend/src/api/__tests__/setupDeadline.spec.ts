import { afterEach, beforeEach, expect, it, vi } from 'vitest'

const ready = { code: 0, data: { needs_setup: false, step: 'completed' } }
function response(json: () => Promise<unknown> = async () => ready) {
  return { ok: true, headers: new Headers({ 'Content-Type': 'application/json' }), json }
}

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.stubEnv('VITE_API_BASE_URL', '/api/v1')
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllEnvs()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it.each(['separated', ''])('bounds stalled headers in deployment mode %j and starts a fresh retry', async (mode) => {
  vi.stubEnv('VITE_DEPLOYMENT_MODE', mode)
  const fetcher = vi.fn().mockImplementationOnce((_url, options) => new Promise((_resolve, reject) => {
    options.signal?.addEventListener('abort', () => reject(options.signal.reason), { once: true })
  })).mockResolvedValueOnce(response())
  vi.stubGlobal('fetch', fetcher)
  const { getSetupStatus } = await import('../setup')
  let outcome: unknown = 'pending'
  const request = getSetupStatus().then(value => { outcome = value }, error => { outcome = error })
  await vi.advanceTimersByTimeAsync(29999)
  expect(outcome).toBe('pending')
  await vi.advanceTimersByTimeAsync(1)
  expect(outcome).toBeInstanceOf(DOMException)
  expect((outcome as Error).name).toBe('TimeoutError')
  await request
  const firstSignal = fetcher.mock.calls[0][1].signal
  expect(firstSignal.aborted).toBe(true)
  expect(vi.getTimerCount()).toBe(0)
  await expect(getSetupStatus()).resolves.toEqual(ready.data)
  const retryOptions = fetcher.mock.calls[1][1]
  expect(retryOptions).toMatchObject({ credentials: 'omit', redirect: 'error', cache: 'no-store' })
  expect(retryOptions.signal).not.toBe(firstSignal)
  expect(retryOptions.signal.aborted).toBe(false)
  expect(vi.getTimerCount()).toBe(0)
})

it('uses one deadline across delayed headers and a stalled body, even if the body ignores abort', async () => {
  let deliverHeaders!: (value: ReturnType<typeof response>) => void
  const fetcher = vi.fn(() => new Promise(resolve => { deliverHeaders = resolve }))
  vi.stubGlobal('fetch', fetcher)
  const { getSetupStatus } = await import('../setup')
  let outcome: unknown = 'pending'
  const request = getSetupStatus().then(value => { outcome = value }, error => { outcome = error })
  await vi.advanceTimersByTimeAsync(20000)
  const body = vi.fn(() => new Promise<unknown>(() => {}))
  deliverHeaders(response(body))
  await vi.advanceTimersByTimeAsync(9999)
  expect(body).toHaveBeenCalledOnce()
  expect(outcome).toBe('pending')
  await vi.advanceTimersByTimeAsync(1)
  expect(outcome).toBeInstanceOf(DOMException)
  expect((outcome as Error).name).toBe('TimeoutError')
  await request
  expect(vi.getTimerCount()).toBe(0)
})

it.each(['success', 'network', 'http', 'json', 'invalid'])('cleans the deadline after %s', async (kind) => {
  const fetcher = vi.fn()
  if (kind === 'network') fetcher.mockRejectedValue(new TypeError('network failed'))
  else if (kind === 'http') fetcher.mockResolvedValue({ ...response(), ok: false })
  else if (kind === 'json') fetcher.mockResolvedValue(response(async () => { throw new SyntaxError('bad JSON') }))
  else if (kind === 'invalid') fetcher.mockResolvedValue(response(async () => ({ code: 0, data: {} })))
  else fetcher.mockResolvedValue(response())
  vi.stubGlobal('fetch', fetcher)
  const { getSetupStatus } = await import('../setup')
  const request = getSetupStatus()
  if (kind === 'success') await expect(request).resolves.toEqual(ready.data)
  else await expect(request).rejects.toThrow()
  expect(vi.getTimerCount()).toBe(0)
  await vi.advanceTimersByTimeAsync(30000)
  expect(fetcher.mock.calls[0][1].signal.aborted).toBe(false)
})

it.each(['before request', 'headers', 'body'])('cancels during %s and removes its deadline/listener', async (phase) => {
  const controller = new AbortController()
  const removed = vi.spyOn(controller.signal, 'removeEventListener')
  const fetcher = vi.fn().mockImplementation(() => phase === 'body'
    ? Promise.resolve(response(() => new Promise(() => {}))) : new Promise(() => {}))
  vi.stubGlobal('fetch', fetcher)
  const { getSetupStatus } = await import('../setup')
  if (phase === 'before request') controller.abort()
  const request = getSetupStatus(controller.signal)
  const rejected = expect(request).rejects.toMatchObject({ name: 'AbortError' })
  await vi.advanceTimersByTimeAsync(1000)
  controller.abort()
  await rejected
  expect(vi.getTimerCount()).toBe(0)
  if (phase === 'before request') expect(fetcher).not.toHaveBeenCalled()
  else {
    expect(fetcher.mock.calls[0][1].signal.aborted).toBe(true)
    expect(removed).toHaveBeenCalledWith('abort', expect.any(Function))
  }
})
