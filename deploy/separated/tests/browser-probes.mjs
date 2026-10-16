import assert from 'node:assert/strict'
import { runVisualAcceptance } from './visual-probes.mjs'
import { writeFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const panel='http://127.0.0.1:8088', gateway='http://127.0.0.1:8089', mock='http://127.0.0.1:8090'
// These helpers are importable for pure, no-listener contract tests.
export async function boundedJSON(url, init={}, fetcher=fetch, timeoutMs=5000, receipt={}) {
 const controller=new AbortController()
 let timer
 try {
  return await Promise.race([
   (async()=>{
    const response=await fetcher(url,{...init,cache:'no-store',signal:controller.signal})
    receipt.httpStatus=response.status
    receipt.raw=await response.text()
    if(!response.ok)throw new Error(`fixture HTTP ${response.status}`)
    const value=JSON.parse(receipt.raw);receipt.value=value;return value
   })(),
   new Promise((_,reject)=>{timer=setTimeout(()=>{controller.abort();reject(new Error('fixture deadline exceeded'))},timeoutMs)}),
  ])
 } catch(error){receipt.error=String(error);throw error}
 finally {clearTimeout(timer)}
}
export function createControlClient(url, trace, fetcher=fetch, timeoutMs=5000) {
 let last=null,failed=false
 return async(value)=>{
  if(failed)throw new Error('control is latched failed')
  const receipt={intended:structuredClone(value)};trace.push(receipt)
  try {
   const ack=await boundedJSON(url,{method:'POST',body:JSON.stringify(value)},fetcher,timeoutMs,receipt)
   assert.ok(Number.isSafeInteger(ack.previousRevision) && ack.previousRevision>=0)
   assert.equal(ack.revision,ack.previousRevision+1)
   if(last)assert.equal(ack.previousRevision,last.revision,'control revision continuity')
   assert.ok(Number.isSafeInteger(ack.sequence) && ack.sequence>=0)
   if(last)assert.ok(ack.sequence>=last.sequence)
   assert.ok(Number.isInteger(ack.status) && ack.status>=200 && ack.status<=599)
   if(value.status!==undefined || last)assert.equal(ack.status,value.status??last.status)
   for(const key of ['setup','setupHang','wsRedirect','visual'])if(value[key]!==undefined)assert.deepEqual(ack[key],value[key])
   receipt.ack=ack;last=ack;return ack
  } catch(error){failed=true;receipt.error=String(error);throw error}
 }
}

export function assertFresh(item) {
  assert.equal(item.after.length,item.before.length+1,item.name+': exactly one new upstream business request')
  const row=item.after.at(-1)
  assert.equal(row.method,'GET');assert.equal(row.url,'/v1/usage')
  assert.equal(row.revision,item.ack.revision);assert.equal(row.status,item.ack.status)
  assert.equal(row.sequence,item.ack.sequence+1)
  assert.equal(row.responseId,`d0-r${row.revision}-s${row.sequence}`)
  assert.deepEqual(item.actual,{status:row.status,id:row.responseId,body:{error:{message:'fixture usage response'},fixture:{revision:row.revision,sequence:row.sequence,status:row.status,responseId:row.responseId,keyTag:row.keyTag}}})
  assert.equal(row.keyTag,item.keyTag)
  assert.equal(row.headers.cookie,undefined)
  assert.ok(!row.rawHeaders.join('|').includes('JWT-SENTINEL'))
 }

async function main() {
const { chromium }=await import('playwright')
const evidence={tests:[],network:[],violations:[],controls:[],stages:[],cache:[],outcome:'failed'}
const control=createControlClient(mock+'/__control',evidence.controls)
const observed=async()=>{
 const result=await boundedJSON(mock+'/__observations')
 assert.ok(Array.isArray(result),'observations must be an array');return result
}
const browser=await chromium.launch({headless:true,args:['--no-sandbox','--host-resolver-rules=MAP * ^NOTFOUND, EXCLUDE 127.0.0.1']})
evidence.browser=browser.version()
try {
 const context=await browser.newContext()
 await context.addCookies([{name:'panel_cookie',value:'COOKIE-SENTINEL',url:panel}])
 const attachTelemetry=async(page,label)=>{
  const session=await page.context().newCDPSession(page)
  await session.send('Network.enable')
  session.on('Network.requestWillBeSent',event=>{
   const headers=Object.fromEntries(Object.entries(event.request.headers).filter(([key])=>['access-control-request-headers','access-control-request-method','origin'].includes(key.toLowerCase())))
   evidence.cache.push({context:label,event:'request',requestId:event.requestId,url:event.request.url,method:event.request.method,headers})
  })
  session.on('Network.responseReceivedExtraInfo',event=>evidence.cache.push({context:label,event:'response-extra',requestId:event.requestId,status:event.statusCode}))
  session.on('Network.requestServedFromCache',event=>evidence.cache.push({context:label,event:'cache-hit',requestId:event.requestId}))
  session.on('Network.responseReceived',event=>evidence.cache.push({context:label,event:'response',requestId:event.requestId,url:event.response.url,status:event.response.status,fromDiskCache:!!event.response.fromDiskCache,fromServiceWorker:!!event.response.fromServiceWorker}))
 }
 const page=await context.newPage()
 await attachTelemetry(page,'primary')
 page.on('request',r=>evidence.network.push({method:r.method(),url:r.url()}))
 await page.addInitScript(()=>{
  window.__violations=[]
  document.addEventListener('securitypolicyviolation',e=>window.__violations.push({uri:e.blockedURI,directive:e.effectiveDirective}))
 })
 // Verify the resolver policy in this browser before business assertions.
 const fixtureResponse=await page.goto(panel+'/__d0__/harness.html',{timeout:15000})
 assert.equal(fixtureResponse.status(),200,'the exact loopback fixture must remain reachable')
 await page.waitForFunction(()=>window.d0)
 evidence.tests.push('resolver allows exact 127.0.0.1 fixture navigation')
 for(const host of ['localhost','127.0.0.2']){
  // A fresh blank page has no fixture CSP that could mask resolver behavior.
  const denied=await context.newPage()
  try {
   let failure
   try {await denied.goto('http://'+host+':8088/__d0__/harness.html',{timeout:10000})}
   catch(error){failure=error}
   assert.ok(failure,'non-exempt host must fail navigation: '+host)
   assert.match(failure.message,/net::ERR_NAME_NOT_RESOLVED(?:\s|$)/,
    'resolver denial must not be a timeout, CSP block or connection refusal')
   evidence.tests.push({name:'resolver rejects non-exempt host',host,error:'net::ERR_NAME_NOT_RESOLVED'})
  } finally {await denied.close()}
 }
 const contract=await page.evaluate(()=>window.d0.getDeploymentContract())
 assert.deepEqual(contract,await (await fetch(panel+'/deployment-manifest.json')).json())
 evidence.harness_contract=contract
 const assertActualAppContract=async()=>{
  const marker=page.locator('[data-testid="app-deployment-contract"]')
  await marker.waitFor({state:'attached'})
  const actual=JSON.parse(await marker.getAttribute('data-contract'))
  assert.deepEqual(actual,contract,'actual App-consumed contract must match the image manifest')
  const entry=await page.locator('script[type="module"][src]').getAttribute('src')
  assert.match(entry,/^\/assets\/index-[A-Za-z0-9_-]+\.js$/)
  evidence.app_contract={actual,entry,source:'App.vue result of its production getDeploymentContract call'}
 }
 await page.evaluate(()=>localStorage.setItem('auth_token','JWT-SENTINEL'))
 await control({clear:true,status:200})
 await page.evaluate(async()=>{
  const b=window.d0.batch,k='API-KEY-SENTINEL'
  await b.submitBatchImageJob(k,{model:'fixture',items:[]},'idem-fixture')
  await b.getBatchImageJob(k,'job');await b.listBatchImageJobs(k);await b.listBatchImageModels(k)
  await b.listBatchImageItems(k,'job');await b.cancelBatchImageJob(k,'job')
  await b.downloadBatchImageZip(k,'job');await b.getBatchImageItemContent(k,'job','item',0)
  await b.deleteBatchImageJobRecord(k,'job')
 })
 let rows=await observed();assert.equal(rows.length,9)
 for(const r of rows){assert.equal(r.headers.authorization,'Bearer API-KEY-SENTINEL');assert.equal(r.headers.cookie,undefined);assert.ok(!r.rawHeaders.join('|').includes('JWT-SENTINEL'))}
 assert.equal(rows[0].headers['idempotency-key'],'idem-fixture')
 assert.ok(!(await context.cookies()).some(c=>c.name==='gateway_fixture'))
 evidence.tests.push('nine production batch calls: CORS, explicit key, no cookies/JWT/Set-Cookie')
 // Full response bodies are consumed, so cacheability is actually exercised.
 const usage=async(target,production=true,key='API-KEY-SENTINEL',extraHeaders={})=>target.evaluate(async({production,key,extraHeaders,gateway})=>{
  try {
   const response=production?await window.d0.gatewayFetch('/v1/usage',key):await fetch(gateway+'/v1/usage',{headers:{Authorization:`Bearer ${key}`,...extraHeaders},credentials:'omit',mode:'cors',redirect:'error'})
   const text=await response.text()
   return {status:response.status,id:response.headers.get('X-Request-Id'),body:JSON.parse(text)}
  } catch(error){return {error:String(error)}}
 },{production,key,extraHeaders,gateway})
 const stage=async(name,target,ack,production=true,key='API-KEY-SENTINEL',extraHeaders={})=>{
  const item={name,ack,production,keyTag:key==='API-KEY-SENTINEL'?'A':'B',before:await observed(),cacheStart:evidence.cache.length}
  evidence.stages.push(item)
  item.actual=await usage(target,production,key,extraHeaders)
  item.after=await observed();item.cacheEnd=evidence.cache.length
  return item
 }
 for(const status of [401,403,429,500]){
  const ack=await control({status})
  assertFresh(await stage('same URL status '+status,page,ack))
 }
 const usagePreflights=evidence.cache.filter(x=>x.context==='primary' && x.event==='request' && x.method==='OPTIONS' && x.url===gateway+'/v1/usage')
 evidence.usage_preflights=usagePreflights
 assert.ok(usagePreflights.length,'first usage must expose its actual OPTIONS request')
 const requestedHeaders=(item)=>Object.entries(item.headers).find(([key])=>key.toLowerCase()==='access-control-request-headers')?.[1].toLowerCase().split(',').map(x=>x.trim()).sort()
 assert.deepEqual(requestedHeaders(usagePreflights[0]),['authorization'])
 const batchOptions=evidence.cache.filter(x=>x.context==='primary' && x.event==='request' && x.method==='OPTIONS' && x.url===gateway+'/v1/images/batches')
 evidence.batch_preflights=batchOptions
 assert.ok(batchOptions.some(x=>JSON.stringify(requestedHeaders(x))===JSON.stringify(['authorization','content-type','idempotency-key'])),'JSON/idempotency batch OPTIONS must retain exactly the three author headers')
 // Two dedicated contexts, each reused for its entire cache scenario. No routing/cache disabling.
 const freshHarness=async(label)=>{
  const ctx=await browser.newContext(),p=await ctx.newPage();await attachTelemetry(p,label)
  await p.goto(panel+'/__d0__/harness.html');await p.waitForFunction(()=>window.d0)
  return {ctx,p}
 }
 const baseline=await freshHarness('cache-baseline')
 let ack=await control({status:403,clear:true})
 const seed=await stage('default cache seed',baseline.p,ack,false);assertFresh(seed)
 ack=await control({status:429})
 const hit=await stage('default must reuse cached 403',baseline.p,ack,false)
 assert.deepEqual(hit.actual,seed.actual);assert.deepEqual(hit.after,hit.before)
 const telemetry=evidence.cache.slice(hit.cacheStart,hit.cacheEnd).filter(x=>x.context==='cache-baseline')
 const usageRequestIds=new Set(telemetry.filter(x=>x.event==='request' && x.url===gateway+'/v1/usage' && x.method==='GET').map(x=>x.requestId))
 assert.ok(telemetry.some(x=>usageRequestIds.has(x.requestId) && (x.event==='cache-hit' || (x.event==='response' && x.fromDiskCache))),'this default usage request must have cache-hit telemetry')
 // No new control here: the same acknowledged 429 must bypass the existing 403 entry.
 assertFresh(await stage('production bypasses existing cached 403',baseline.p,ack))
 await baseline.ctx.close()
 const insertion=await freshHarness('no-store-insertion')
 ack=await control({status:200,clear:true});assertFresh(await stage('production no-store 200 A',insertion.p,ack))
 ack=await control({status:200});assertFresh(await stage('default fetch after no-store must reach revision B',insertion.p,ack,false))
 ack=await control({status:200});assertFresh(await stage('dynamic production 200 with key A',insertion.p,ack))
 ack=await control({status:200});assertFresh(await stage('dynamic production 200 with key B',insertion.p,ack,true,'API-KEY-B-SENTINEL'))
 await insertion.ctx.close()
 for(const name of ['Cache-Control','Pragma','X-Untrusted']){
  ack=await control({clear:true,status:200})
  const denied=await stage('author header denied '+name,page,ack,false,'API-KEY-SENTINEL',{[name]:'no-cache'})
  assert.ok(denied.actual.error,'CORS rejection must not be a readable upstream response')
  assert.deepEqual(denied.after,denied.before,'denied preflight must not reach upstream')
  const options=evidence.cache.slice(denied.cacheStart,denied.cacheEnd).filter(x=>x.event==='request' && x.method==='OPTIONS' && x.url===gateway+'/v1/usage')
  assert.ok(options.some(x=>requestedHeaders(x)?.includes(name.toLowerCase())),'capture rejected author-header OPTIONS')
  assert.ok(options.some(x=>evidence.cache.slice(denied.cacheStart,denied.cacheEnd).some(y=>['response','response-extra'].includes(y.event) && y.requestId===x.requestId && y.status===403)),'actual preflight 403 required')
 }
 evidence.tests.push('same-URL status/revision/ID, real default cache hit, no-store bypass/insertion, dynamic keys, strict author cache-header rejection')
 await control({status:200,clear:true})
 assert.equal(await page.evaluate(async()=>{try{await window.d0.gatewayFetch('/v1/images/batches/redirect','API-KEY-SENTINEL');return false}catch{return true}}),true)
 assert.equal((await observed()).filter(r=>r.url==='/leak').length,0)
 evidence.tests.push('actual 4xx/5xx readable and redirect destination receives zero requests')
 await control({clear:true})
 await page.evaluate(async()=>{
  window.__wsStatus=[]
  const stop=window.d0.subscribeQPS(()=>{},{token:'ADMIN-JWT-SENTINEL',maxReconnectAttempts:0,onStatusChange:s=>window.__wsStatus.push(s)})
  await new Promise(r=>setTimeout(r,600));stop()
 })
 rows=await observed();const ws=rows.find(r=>r.method==='WS');assert.ok(ws)
 assert.equal(ws.url,'/api/v1/admin/ops/ws/qps');assert.match(ws.headers['sec-websocket-protocol'],/jwt.ADMIN-JWT-SENTINEL/)
 await control({clear:true})
 await page.evaluate(()=>window.d0.subscribeQPS(()=>{},{wsBaseUrl:'127.0.0.1:8090/evil',token:'ADMIN-JWT-SENTINEL'})())
 assert.equal((await observed()).length,0)
 await page.evaluate(async()=>{try{await fetch('http://127.0.0.1:8090/csp-denied')}catch{};try{new WebSocket('ws://127.0.0.1:8090/csp-ws')}catch{}})
 evidence.violations=await page.evaluate(()=>window.__violations)
 assert.ok(evidence.violations.some(v=>v.uri==='http://127.0.0.1:8090/csp-denied' && v.directive==='connect-src'))
 assert.ok(evidence.violations.some(v=>v.uri==='ws://127.0.0.1:8090/csp-ws' && v.directive==='connect-src'))
 assert.equal((await observed()).length,0)
 await control({clear:true,wsRedirect:true})
 await page.evaluate(async()=>{const stop=window.d0.subscribeQPS(()=>{},{token:'ADMIN-JWT-SENTINEL',maxReconnectAttempts:0});await new Promise(r=>setTimeout(r,600));stop()})
 assert.equal((await observed()).filter(r=>r.url==='/ws-leak').length,0)
 await control({wsRedirect:false})
 evidence.tests.push('panel WS fixed destination and CSP rejects third HTTP/WS origin')
 const third=await context.newPage();await third.goto(mock+'/third');await control({clear:true})
 assert.equal(await third.evaluate(async(g)=>{try{await fetch(g+'/v1/usage',{headers:{Authorization:'Bearer SENTINEL'}});return false}catch{return true}},gateway),true)
 assert.equal((await observed()).length,0)
 // Actual production App, router and KeyUsage component (no request interception).
 for(const mode of ['http500','headers','body']){
  await control({setup:{code:0,data:{needs_setup:false,step:'completed'}},status:mode==='http500'?500:200,setupHang:mode==='http500'?'':mode})
  const started=Date.now()
  await page.goto(panel+'/key-usage')
  await assertActualAppContract()
  const unavailable=page.locator('[data-testid="setup-unavailable"]')
  await unavailable.waitFor()
  assert.equal(await page.locator('input[type=password]').count(),0)
  await unavailable.locator('button').waitFor({timeout:45000})
  const elapsed=Date.now()-started
  if(mode!=='http500')assert.ok(elapsed>=29000 && elapsed<45000,'real status deadline must bound loading')
  await control({status:200,setupHang:''})
  await unavailable.locator('button').click()
  await page.locator('input[type=password]').waitFor()
  await assertActualAppContract()
  evidence.tests.push({name:'real App error/deadline and retry recovery',mode,elapsed_ms:elapsed})
 }
 await control({setup:{code:0,data:{needs_setup:true,step:'welcome'}},status:200})
 await page.goto(panel+'/key-usage');await assertActualAppContract();await page.locator('[data-testid="setup-unavailable"]').waitFor()
 assert.equal(await page.locator('input[type=password]').count(),0)
 await control({setup:{code:0,data:{needs_setup:'false',step:'completed'}}})
 await page.locator('[data-testid="setup-unavailable"] button').click();await page.waitForTimeout(200)
 assert.equal(await page.locator('[data-testid="setup-unavailable"]').count(),1)
 await control({setup:{code:0,data:{needs_setup:false,step:'completed'}}})
 await page.locator('[data-testid="setup-unavailable"] button').click()
 const input=page.locator('input[type=password]');await input.waitFor();await input.fill('API-KEY-SENTINEL')
 await page.evaluate(()=>localStorage.setItem('auth_token','JWT-SENTINEL'))
 await control({clear:true,status:401})
 await input.locator('xpath=../..').locator('button').last().click()
 await page.waitForTimeout(500)
 rows=await observed();const appUsage=rows.find(r=>r.url.startsWith('/v1/usage'));assert.ok(appUsage)
 assert.equal(appUsage.headers.authorization,'Bearer API-KEY-SENTINEL');assert.equal(appUsage.headers.cookie,undefined)
 evidence.tests.push('real App gates true/invalid status, retry restores real KeyUsage; tenth call has no panel credentials')
 // Preserve the original D0 observations before the additional visual phase.
 evidence.final_observations=await observed()
 evidence.visual=await runVisualAcceptance(browser,{panel,gateway,control,observed,contract})
 assert.equal(evidence.visual.outcome,'passed')
 evidence.outcome='passed'
} catch(error) {
 evidence.error={message:error.message,stack:error.stack};throw error
} finally {
 try {if(!evidence.final_observations)evidence.final_observations=await observed()}
 catch(error){evidence.observation_error=String(error)}
 // Neither supplemental evidence nor browser cleanup may replace the primary failure.
 try {await writeFile('/evidence/browser.json',JSON.stringify(evidence,null,2))}
 catch(error){if(!evidence.error)throw error;console.error('evidence write failed',String(error))}
 finally {try{await browser.close()}catch(error){if(!evidence.error)throw error;console.error('browser close failed',String(error))}}
}
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href)await main()
