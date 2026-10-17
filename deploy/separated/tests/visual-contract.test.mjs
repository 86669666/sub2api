import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { visualResponse, trendFixture } from './visual-data.mjs'
import { Readable } from 'node:stream'
import { createFixtureState, createFixtureServer } from './mock-upstream.mjs'
import { assertTableSnapshot, assertStableScroll, assertChartSnapshot, assertThemeTransition, cacheRasterEvidence, assertTrendRegion, pageCases, viewports } from './visual-contract.mjs'
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
// Synthetic RGBA inputs exercise the verifier only; browser evidence must come
// from the same-canvas draw-ablation probe, never from these generated samples.
const names=['Input','Output','Cache Creation','Cache Read','Cache Hit Rate']
const trace=indices=>[...indices].reverse().map(index=>({index,label:names[index]}))
const rgba=(pixels,color=[100,100,100,255])=>{
 const bytes=Array(13*13*4).fill(0)
 // Fixed grid/fill contamination appears in every frame and must cancel.
 for(const n of [0,1,2,3])bytes.splice(n*4,4,50,50,50,255)
 for(const n of pixels)bytes.splice(n*4,4,...color)
 return bytes
}
const shapes=[[57,69,70,71,81,85],[58,68,72,80,86,94]]
const raster=()=>({method:'same-canvas-controller-draw-ablation-v1',backdrop:[250,250,250],restored:true,fullDraws:trace([0,1,2,3,4]),baselineDraws:[],targets:[2,3].map((index,k)=>({index,label:names[index],isolatedDraws:trace([index]),withoutDraws:trace([0,1,2,3,4].filter(i=>i!==index)),patches:[{pointIndex:0,x:60+k*10,y:60+k*10,width:13,height:13,baseline:rgba([]),isolated:rgba(shapes[k]),full:rgba(shapes.flat()),without:rgba(shapes[1-k])}]}))})
const chart=()=>({id:1,sameCanvas:true,width:300,height:200,ink:500,pixelHash:123,chartArea:{top:30,bottom:190,left:30,right:290},datasets:[{data:[20],points:[{x:50,y:50,ink:9}]},{data:[5]},{label:'Cache Creation',data:[10],dash:[2,3],point:'triangle',points:[{x:60,y:60,ink:6}]},{label:'Cache Read',data:[90],dash:[8,3],point:'rectRot',points:[{x:70,y:70,ink:6}]},{data:[75]}],legend:[{text:'Cache Creation',point:'triangle',box:{width:30,height:10}},{text:'Cache Read',point:'rectRot',box:{width:30,height:10}}],cacheRaster:raster(),colors:Object.fromEntries(['line','fill','point','muted','faint','hitRate','legend','xGrid','yGrid','xText','yText','percentText'].map(k=>[k,'light']))})
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

test('023 P2-01: zero/missing cache pixels cannot borrow Input, grid, or theme pixel changes',()=>{
 const old=chart();delete old.cacheRaster
 old.datasets[2].points[0].ink=0;old.datasets[3].points[0].ink=0
 assert.throws(()=>assertChartSnapshot(old))
 const after=structuredClone(old);after.pixelHash=456;after.colors=Object.fromEntries(Object.keys(after.colors).map(k=>[k,'dark']))
 assert.throws(()=>assertThemeTransition(old,after))
 for(const corrupt of [
  s=>delete s.cacheRaster,
  s=>delete s.datasets[2].points,
  s=>s.datasets[2].points[0].ink=0,
  s=>s.datasets[3].points[0].ink=0,
  s=>s.cacheRaster.targets[0].patches=[],
  s=>delete s.cacheRaster.targets[0].patches[0].isolated,
  s=>s.cacheRaster.targets[0].patches[0].isolated=rgba([]),
  s=>s.cacheRaster.targets[0].patches[0].isolated=rgba(shapes[0],[100,100,100,0]),
  s=>s.cacheRaster.targets[0].patches[0].isolated=rgba(shapes[0],[250,250,250,255]),
  s=>s.cacheRaster.targets[0].patches[0].isolated=rgba(shapes[0],[245,245,245,255]),
  s=>s.cacheRaster.targets[0].patches[0].without=[...s.cacheRaster.targets[0].patches[0].full],
  s=>s.cacheRaster.targets[0].isolatedDraws=trace([0]),
  s=>s.cacheRaster.targets[0].index=3,
  s=>s.cacheRaster.targets[0].patches[0].x=999,
  s=>s.cacheRaster.restored=false,
 ]){const s=chart();corrupt(s);assert.throws(()=>assertChartSnapshot(s))}
 for(const index of [0,1]){
  const s=chart(),p=s.cacheRaster.targets[index].patches[0]
  p.isolated=[...p.baseline];p.full=[...p.without]
  assert.throws(()=>assertChartSnapshot(s),/contrasting/)
 }
})
test('cache raster evidence requires contrasting, attributable, distinct marks in actual composite',()=>{
 const good=raster();const result=cacheRasterEvidence(good)
 assert.deepEqual(result.targets.map(t=>t.visiblePixels),[6,6]);assert.deepEqual(result.exclusive,[6,6])
 const identical=raster()
 for(const t of identical.targets){t.patches[0].isolated=rgba(shapes[0]);t.patches[0].full=rgba(shapes[0]);t.patches[0].without=rgba([])}
 assert.throws(()=>cacheRasterEvidence(identical),/distinguishable/)
 const erasure=raster();const p=erasure.targets[0].patches[0]
 p.baseline=rgba(shapes[0],[20,20,20,255]);p.isolated=rgba(shapes[0],[250,250,250,255]);p.full=[...p.isolated];p.without=[...p.baseline]
 assert.throws(()=>cacheRasterEvidence(erasure),/contrasting/,'erasing grid with backdrop color is not a visible cache curve')
})
const trendState=mode=>({response:{path:'/api/v1/usage/dashboard/trend',method:'GET',status:200,body:visualResponse(req('/api/v1/usage/dashboard/trend',{authorization:'Bearer VISUAL-user-TOKEN'}),mode).body},regionCount:1,heading:'Token Usage Trend',visible:true,loading:false,emptyText:mode==='empty'?'No data available':null,canvasCount:mode==='empty'?0:1,canvasInk:mode==='empty'?0:500,previousCanvasConnected:false})
test('023 P2-02: normal model-card empty text cannot satisfy trend empty state',()=>{
 const headers={authorization:'Bearer VISUAL-user-TOKEN'}
 for(const mode of ['normal','empty'])assert.deepEqual(visualResponse(req('/api/v1/usage/dashboard/models',headers),mode).body.data.models,[])
 const normal=trendState('normal');assertTrendRegion(normal,'normal')
 assert.equal(normal.response.body.data.trend.length,3)
 normal.mainText='No data available';assert.throws(()=>assertTrendRegion(normal,'empty'))
 const empty=trendState('empty');assertTrendRegion(empty,'empty')
 for(const corrupt of [
  s=>s.response=trendState('normal').response,
  s=>delete s.response.body.data.trend,
  s=>s.response.path='/api/v1/usage/dashboard/models',
  s=>s.response.status=500,
  s=>s.regionCount=2,
  s=>s.heading='Model Distribution',
  s=>s.emptyText=null,
  s=>s.canvasCount=1,
  s=>s.previousCanvasConnected=true,
  s=>s.loading=true,
 ]){const s=trendState('empty');corrupt(s);assert.throws(()=>assertTrendRegion(s,'empty'))}
 const missingCurve=trendState('normal');missingCurve.canvasInk=0;assert.throws(()=>assertTrendRegion(missingCurve,'normal'))
})
