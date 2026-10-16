import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { visualResponse, trendFixture } from './visual-data.mjs'
import { Readable } from 'node:stream'
import { createFixtureState, createFixtureServer } from './mock-upstream.mjs'
import { assertTableSnapshot, assertStableScroll, assertChartSnapshot, assertThemeTransition, pageCases, viewports } from './visual-contract.mjs'
const req=(url,headers={},method='GET',body='')=>({url,headers,method,body,rawHeaders:[]})
test('visual mode is opt-in and cannot change default D0 responses',()=>{
 const state=createFixtureState('test');assert.equal(state.select(req('/v1/usage')).visual,'off')
 assert.equal(visualResponse(req('/v1/usage'),'off'),null)
 assert.throws(()=>state.control({visual:'arbitrary'}))
 const ack=state.control({visual:'normal'});assert.equal(ack.visual,'normal')
 const selected=state.select(req('/api/v1/settings/public'));state.control({visual:'off'})
 assert.equal(selected.visual,'normal','in-flight snapshot preserved');assert.equal(state.select(req('/v1/usage')).visual,'off')
})
test('normal form login issues role-bound fixture identity; no auth bypass or mutations',()=>{
 const url='/api/v1/auth/login'
 assert.equal(visualResponse(req(url,{},'POST','{}'),'normal').status,401)
 for(const role of ['user','admin']){
  const r=visualResponse(req(url,{},'POST',JSON.stringify({email:role+'@fixture.invalid',password:'fixture-password'})),'normal')
  assert.equal(r.status,200);assert.equal(r.body.data.user.role,role)
  const headers={authorization:'Bearer '+r.body.data.access_token}
  assert.equal(visualResponse(req('/api/v1/auth/me',headers),'normal').body.data.role,role)
  assert.equal(visualResponse(req('/api/v1/admin/settings',headers),'normal').status,role==='admin'?200:403)
  assert.equal(visualResponse(req('/api/v1/user/profile',{},'GET'),'normal').status,401)
  assert.equal(visualResponse(req('/api/v1/keys',headers,'POST','{}'),'normal').status,405)
  assert.equal(visualResponse(req('/api/v1/unmapped',headers),'normal').status,501)
 }
})
test('API key route rejects panel bearer and controlled modes produce distinct real HTTP states',()=>{
 assert.equal(visualResponse(req('/v1/usage',{authorization:'Bearer VISUAL-admin-TOKEN'}),'normal').status,401)
 const request=req('/v1/usage',{authorization:'Bearer API-KEY-SENTINEL'})
 assert.equal(visualResponse(request,'normal').status,200);assert.equal(visualResponse(request,'error').status,429)
 assert.ok(visualResponse(request,'loading').delay>=1000)
 const admin={authorization:'Bearer VISUAL-admin-TOKEN'}
 assert.deepEqual(visualResponse(req('/api/v1/admin/dashboard/snapshot-v2',admin),'empty').body.data.trend,[])
 assert.equal(visualResponse(req('/api/v1/admin/settings',admin),'error').status,500)
})
test('matrix retains required route widths, crossing/overlap/zero data and existing isolation',()=>{
 assert.deepEqual(viewports,[320,768,1440]);assert.equal(pageCases.length,7)
 for(const kind of ['crossing','overlap','zero'])assert.equal(trendFixture(kind).length,3)
 const cross=trendFixture();assert.ok(cross[0].cache_creation_tokens<cross[0].cache_read_tokens&&cross[1].cache_creation_tokens>cross[1].cache_read_tokens)
 assert.ok(trendFixture('zero').every(x=>x.input_tokens+x.cache_creation_tokens+x.cache_read_tokens===0))
 const docker=readFileSync(new URL('../Dockerfile.frontend',import.meta.url),'utf8')
 assert.ok(docker.endsWith('FROM panel AS production\n'))
 assert.ok(!docker.slice(0,docker.indexOf('FROM frontend-builder AS harness-builder')).includes('COPY --from=harness-builder'))
})
const table=()=>({virtual:true,ids:[1,2,3,4],rows:[{id:'2',index:1,top:10,bottom:40,height:30},{id:'3',index:2,top:40,bottom:70,height:30}],visible:[{top:10,bottom:40},{top:40,bottom:70}],contentTop:20,bottom:60,lastDataBottom:70,scrollHeight:120,clientHeight:40})
test('table verifier rejects blank windows, overlap, stale identity, lost rows and unsettled scroll',()=>{
 assertTableSnapshot(table(),{virtual:true})
 for(const corrupt of [s=>s.visible=[],s=>s.rows[1].top=30,s=>s.rows[0].id='999',s=>s.rows[1].index=3]){const s=table();corrupt(s);assert.throws(()=>assertTableSnapshot(s,{virtual:true}))}
 assert.throws(()=>assertTableSnapshot(table(),{virtual:false}));assert.throws(()=>assertTableSnapshot(table(),{virtual:true,atEnd:true}))
 assertStableScroll([100,101,100]);assert.throws(()=>assertStableScroll([100,104,108]))
})
const chart=()=>({id:1,sameCanvas:true,width:300,height:200,ink:500,pixelHash:123,chartArea:{top:30,bottom:190,left:30,right:290},datasets:[{data:[20],points:[{x:50,y:50,ink:9}]},{data:[5]},{data:[10],dash:[2,3],point:'triangle'},{data:[90],dash:[8,3],point:'rectRot'},{data:[75]}],legend:[{text:'Cache Creation',point:'triangle',box:{width:30,height:10}},{text:'Cache Read',point:'rectRot',box:{width:30,height:10}}],colors:Object.fromEntries(['line','fill','point','muted','faint','hitRate','legend','xGrid','yGrid','xText','yText','percentText'].map(k=>[k,'light']))})
test('chart verifier rejects unpainted canvases, identical markers, stale colors and remounts',()=>{
 assertChartSnapshot(chart())
 for(const corrupt of [s=>s.ink=0,s=>s.datasets[2].dash=[8,3],s=>s.legend[0].point='rectRot',s=>s.datasets[4].data=[100]]){const s=chart();corrupt(s);assert.throws(()=>assertChartSnapshot(s))}
 const before=chart(),after=chart();after.pixelHash=456;after.colors=Object.fromEntries(Object.keys(after.colors).map(k=>[k,'dark']))
 assertThemeTransition(before,after)
 for(const corrupt of [s=>s.id=2,s=>s.sameCanvas=false,s=>s.pixelHash=123,s=>s.colors.line='light']){const s=structuredClone(after);corrupt(s);assert.throws(()=>assertThemeTransition(before,s))}
})

// Real server handler, in-memory streams only; no listener, socket or provider.
const invoke=(server,url,{method='GET',headers={},body=''}={})=>new Promise((resolve,reject)=>{
 const request=Readable.from(body?[body]:[]);Object.assign(request,{url,method,headers,rawHeaders:Object.entries(headers).flat()})
 const response={statusCode:200,headers:{},setHeader(k,v){this.headers[k.toLowerCase()]=v},end(value=''){resolve({status:this.statusCode,headers:this.headers,body:JSON.parse(String(value))})}}
 Promise.resolve(server.listeners('request')[0](request,response)).catch(reject)
})
test('real handler isolates visual auth/no-store responses and restores original D0 cache contract',async()=>{
 const server=createFixtureServer('visual-test')
 await invoke(server,'/__control',{method:'POST',body:JSON.stringify({visual:'normal'})})
 const login=await invoke(server,'/api/v1/auth/login',{method:'POST',body:JSON.stringify({email:'user@fixture.invalid',password:'fixture-password'})})
 assert.equal(login.status,200);assert.equal(login.headers['cache-control'],'no-store')
 const auth={authorization:'Bearer '+login.body.data.access_token}
 assert.equal((await invoke(server,'/api/v1/usage/dashboard/stats',{headers:auth})).status,200)
 assert.equal((await invoke(server,'/api/v1/admin/settings',{headers:auth})).status,403)
 const rows=(await invoke(server,'/__observations')).body
 assert.equal(rows.at(-1).status,403);assert.equal(rows.at(-1).visual,'normal')
 await invoke(server,'/__control',{method:'POST',body:JSON.stringify({visual:'off',status:403})})
 const original=await invoke(server,'/v1/usage',{headers:{authorization:'Bearer API-KEY-SENTINEL'}})
 assert.equal(original.status,403);assert.equal(original.headers['cache-control'],'public, max-age=3600');assert.equal(original.body.fixture.keyTag,'A')
 assert.equal(server.listening,false)
})
