import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
const panel='http://127.0.0.1:8088', gateway='http://127.0.0.1:8089', mock='http://127.0.0.1:8090'
const control=async(value)=>fetch(mock+'/__control',{method:'POST',body:JSON.stringify(value)})
const observed=async()=> (await fetch(mock+'/__observations')).json()
const browser=await chromium.launch({headless:true,args:['--no-sandbox','--host-resolver-rules=MAP * ~NOTFOUND']})
const evidence={browser:browser.version(),tests:[],network:[],violations:[],outcome:'failed'}
try {
 const context=await browser.newContext()
 await context.addCookies([{name:'panel_cookie',value:'COOKIE-SENTINEL',url:panel}])
 const page=await context.newPage()
 page.on('request',r=>evidence.network.push({method:r.method(),url:r.url()}))
 await page.addInitScript(()=>{
  window.__violations=[]
  document.addEventListener('securitypolicyviolation',e=>window.__violations.push({uri:e.blockedURI,directive:e.effectiveDirective}))
 })
 await page.goto(panel+'/__d0__/harness.html');await page.waitForFunction(()=>window.d0)
 const contract=await page.evaluate(()=>window.d0.getDeploymentContract())
 assert.deepEqual(contract,await (await fetch(panel+'/deployment-manifest.json')).json())
 evidence.contract=contract
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
 for(const status of [401,403,429,500]){
  await control({status})
  assert.deepEqual(await page.evaluate(async()=>{const r=await window.d0.gatewayFetch('/v1/usage','API-KEY-SENTINEL');return [r.status,r.headers.get('X-Request-Id')] }),[status,'d0-fixture-id'])
 }
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
 assert.ok(evidence.violations.some(v=>v.uri.includes('8090')))
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
 await control({setup:{code:0,data:{needs_setup:true,step:'welcome'}},status:200})
 await page.goto(panel+'/key-usage');await page.locator('[data-testid="setup-unavailable"]').waitFor()
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
 rows=await observed();const usage=rows.find(r=>r.url.startsWith('/v1/usage'));assert.ok(usage)
 assert.equal(usage.headers.authorization,'Bearer API-KEY-SENTINEL');assert.equal(usage.headers.cookie,undefined)
 evidence.tests.push('real App gates true/invalid status, retry restores real KeyUsage; tenth call has no panel credentials')
 evidence.outcome='passed'
} finally {
 await writeFile('/evidence/browser.json',JSON.stringify(evidence,null,2))
 await browser.close()
}
