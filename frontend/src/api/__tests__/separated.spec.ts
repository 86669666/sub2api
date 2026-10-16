import { beforeEach, afterEach, expect, it, vi } from 'vitest'
beforeEach(() => {
  vi.resetModules(); vi.stubEnv('VITE_API_BASE_URL','/api/v1'); vi.stubEnv('VITE_DEPLOYMENT_MODE','separated')
  vi.stubEnv('VITE_PANEL_PUBLIC_ORIGIN','http://localhost:3000'); vi.stubEnv('VITE_GATEWAY_BASE_URL','http://localhost:8089')
  localStorage.setItem('auth_token','PANEL-JWT-SENTINEL')
})
afterEach(() => {vi.unstubAllEnvs();vi.unstubAllGlobals();vi.restoreAllMocks()})
it('separates gateway from panel root and retains monolith defaults',async()=>{
  const u=await import('../url')
  expect(u.buildGatewayUrl('/v1/usage?q=a%2Fb')).toBe('http://localhost:8089/v1/usage?q=a%2Fb')
  expect(u.buildPanelRootUrl('/setup/status')).toBe(window.location.origin+'/setup/status')
  for(const x of ['//evil','/v1/../api/v1','/v1/%2e/usage','/v1/images/batches/a%252fb']) expect(()=>u.buildGatewayUrl(x)).toThrow()
  vi.stubEnv('VITE_GATEWAY_BASE_URL','');expect(()=>u.buildGatewayUrl('/v1/usage')).toThrow()
  vi.stubEnv('VITE_DEPLOYMENT_MODE','');expect(u.buildGatewayUrl('/v1/usage')).toBe(window.location.origin+'/v1/usage')
  vi.resetModules();vi.stubEnv('VITE_API_BASE_URL','https://panel.example:8443/api/v1')
  expect((await import('../url')).buildGatewayUrl('v1/usage')).toBe('https://panel.example:8443/v1/usage')
})
it('nine batch operations use only the explicit key and fixed fetch policy',async()=>{
  const fetcher=vi.fn().mockImplementation(async()=>({ok:true,json:async()=>({}),blob:async()=>new Blob()}));vi.stubGlobal('fetch',fetcher)
  const b=await import('../batchImage')
  await b.submitBatchImageJob('KEY',{model:'fixture',items:[]},'IDEM')
  await b.getBatchImageJob('KEY','job');await b.listBatchImageJobs('KEY');await b.listBatchImageModels('KEY')
  await b.listBatchImageItems('KEY','job');await b.cancelBatchImageJob('KEY','job');await b.downloadBatchImageZip('KEY','job')
  await b.getBatchImageItemContent('KEY','job','item',0);await b.deleteBatchImageJobRecord('KEY','job')
  expect(fetcher).toHaveBeenCalledTimes(9)
  for(const [url,init] of fetcher.mock.calls){expect(url).toMatch(/^http:\/\/localhost:8089\/v1\/images\/batches/);expect(init).toMatchObject({credentials:'omit',redirect:'error',mode:'cors'});expect(init.headers.Authorization).toBe('Bearer KEY');expect(JSON.stringify(init)).not.toContain('PANEL-JWT')}
  expect(fetcher.mock.calls[0][1].headers).toEqual({Authorization:'Bearer KEY','Content-Type':'application/json','Idempotency-Key':'IDEM'})
  fetcher.mockResolvedValueOnce({ok:false,status:429,statusText:'limit',headers:new Headers({'X-Request-Id':'fixture-id'}),json:async()=>({error:{message:'limit'}})})
  await expect(b.getBatchImageJob('KEY','job')).rejects.toMatchObject({requestId:'fixture-id'})
  await expect(b.getBatchImageJob('KEY','../escape')).rejects.toThrow()
})
it('strict status and write rejection before requests',async()=>{
  const f=vi.fn();vi.stubGlobal('fetch',f);const api=await import('../setup')
  for(const result of [{},{code:0,data:{needs_setup:'false',step:'completed'}},{code:1,data:{needs_setup:false,step:'completed'}}]){f.mockResolvedValueOnce({ok:true,headers:new Headers({'Content-Type':'application/json'}),json:async()=>result});await expect(api.getSetupStatus()).rejects.toThrow()}
  f.mockResolvedValueOnce({ok:true,headers:new Headers({'Content-Type':'application/json; charset=utf-8'}),json:async()=>({code:0,data:{needs_setup:false,step:'completed'}})})
  expect(await api.getSetupStatus()).toEqual({needs_setup:false,step:'completed'})
  expect(f.mock.calls.at(-1)).toEqual([window.location.origin+'/setup/status',{credentials:'omit',redirect:'error',cache:'no-store',signal:expect.any(AbortSignal)}])
  const calls=f.mock.calls.length
  await expect(api.install({} as any)).rejects.toThrow('disabled');await expect(api.testDatabase({} as any)).rejects.toThrow('disabled');await expect(api.testRedis({} as any)).rejects.toThrow('disabled');expect(f).toHaveBeenCalledTimes(calls)
})
it('WS validates fixed path before token read and closes invalid attempts',async()=>{
  const u=await import('../url');expect(u.buildAdminWSUrl().pathname).toBe('/api/v1/admin/ops/ws/qps')
  for(const x of ['evil.example','localhost:8089','localhost:3000/x','localhost:3000?','localhost:3000#','u@localhost:3000','ws://localhost:3000'])expect(()=>u.buildAdminWSUrl(x)).toThrow()
  const token=vi.fn(()=> 'JWT'),status=vi.fn(),socket=vi.fn();vi.stubGlobal('WebSocket',socket)
  const stop=(await import('../admin/ops')).subscribeQPS(()=>{},{wsBaseUrl:'evil.example',onStatusChange:status,get token(){return token()}})
  expect(token).not.toHaveBeenCalled();expect(socket).not.toHaveBeenCalled();expect(status).toHaveBeenLastCalledWith('closed');stop()
})

it('rejects an absolute panel API before an Axios client can attach a JWT',async()=>{
 vi.stubEnv('VITE_API_BASE_URL','https://wrong.example/api/v1')
 await expect(import('../client')).rejects.toThrow('Separated panel API')
})
