import test from 'node:test'
import assert from 'node:assert/strict'
import { Readable } from 'node:stream'
import { createFixtureState, createFixtureServer } from './mock-upstream.mjs'
import { boundedJSON, createControlClient, assertFresh } from './browser-probes.mjs'
const req=(key='API-KEY-SENTINEL')=>({method:'GET',url:'/v1/usage',headers:{authorization:`Bearer ${key}`},rawHeaders:['Authorization',`Bearer ${key}`]})
const reply=(value,status=200)=>new Response(typeof value==='string'?value:JSON.stringify(value),{status})

test('applied control acknowledgement and immutable per-request identity survive clear/key changes',()=>{
 const state=createFixtureState()
 const a=state.control({status:403}),first=state.select(req())
 assert.deepEqual([a.previousRevision,a.revision,a.sequence,a.status],[0,1,0,403])
 assert.deepEqual([first.row.sequence,first.row.revision,first.row.status,first.row.responseId,first.row.keyTag],[1,1,403,'d0-r1-s1','A'])
 const b=state.control({status:429,clear:true}),second=state.select(req('API-KEY-B-SENTINEL'))
 assert.deepEqual([b.previousRevision,b.revision,b.sequence,b.status],[1,2,1,429])
 assert.deepEqual([second.row.sequence,second.row.revision,second.row.status,second.row.responseId,second.row.keyTag],[2,2,429,'d0-r2-s2','B'])
 assert.equal(state.requests.length,1);assert.equal(first.row.status,403)
 assert.notEqual(first.row.responseId,second.row.responseId)
})
test('selection freezes setup and redirect status before later controls',()=>{
 const state=createFixtureState(),value={code:0,data:{needs_setup:false}}
 state.control({setup:value,status:500,setupHang:'body'})
 value.data.needs_setup=true
 const first=state.select({...req(),url:'/setup/status'})
 state.control({setup:{code:1},setupHang:'',wsRedirect:true})
 assert.equal(first.setup.data.needs_setup,false);assert.equal(first.setupHang,'body');assert.equal(first.row.status,500)
 assert.equal(state.select({...req(),url:'/v1/images/batches/redirect'}).row.status,302)
 assert.equal(state.select(req(),'',true).row.status,302)
})
test('invalid controls fail before changing any fixture state',()=>{
 const state=createFixtureState()
 state.control({status:403});state.select(req())
 for(const invalid of [{status:0,clear:true},{status:429,setupHang:'invalid'},{wsRedirect:'true'},[],null])assert.throws(()=>state.control(invalid))
 const ack=state.control({})
 assert.deepEqual([ack.revision,ack.status,ack.sequence,state.requests.length],[2,403,1,1])
})
test('checked control consumes body, preserves no-store, and validates continuity',async()=>{
 const state=createFixtureState(),trace=[],calls=[]
 const client=createControlClient('http://fixture/__control',trace,async(url,init)=>{
  calls.push({url,init});return reply(state.control(JSON.parse(init.body)))
 })
 const ack=await client({status:403});await client({clear:true});await client({status:429})
 assert.equal(ack.status,403);assert.equal(trace[2].ack.revision,3)
 assert.equal(calls[0].init.cache,'no-store');assert.ok(calls[0].init.signal instanceof AbortSignal)
 assert.equal(trace[0].httpStatus,200);assert.ok(trace[0].raw.includes('403'))
})
for(const [name,response] of [
 ['HTTP failure',()=>reply({error:'injected'},503)],
 ['malformed JSON',()=>reply('{')],
 ['wrong revision',()=>reply({previousRevision:0,revision:7,sequence:0,status:429})],
 ['wrong status',()=>reply({previousRevision:0,revision:1,sequence:0,status:403})],
 ['missing sequence',()=>reply({previousRevision:0,revision:1,status:429})],
])test(`bad control ${name} fails closed before business and retains evidence`,async()=>{
 let business=0,calls=0;const trace=[]
 const client=createControlClient('http://fixture/__control',trace,async()=>{calls++;return response()})
 await assert.rejects(async()=>{await client({status:429});business++})
 assert.equal(business,0);assert.equal(trace.length,1);assert.deepEqual(trace[0].intended,{status:429});assert.ok(trace[0].error)
 await assert.rejects(()=>client({status:200}),/latched failed/);assert.equal(calls,1)
})
test('stale acknowledgement rejected even when status matches',async()=>{
 const trace=[],ack={previousRevision:0,revision:1,sequence:0,status:200}
 const client=createControlClient('http://fixture/__control',trace,async()=>reply(ack))
 await client({status:200});await assert.rejects(()=>client({status:200}),/continuity/)
 assert.equal(trace.length,2);assert.ok(trace[1].error)
})
test('bounded control includes stalled body and does not continue business',async()=>{
 let business=0;const trace=[]
 const client=createControlClient('http://fixture/__control',trace,async()=>({ok:true,status:200,text:()=>new Promise(()=>{})}),15)
 await assert.rejects(async()=>{await client({status:200});business++},/deadline/)
 assert.equal(business,0);assert.equal(trace[0].httpStatus,200);assert.match(trace[0].error,/deadline/)
})
test('observation fetch is also bounded and never uses HTTP cache',async()=>{
 let init
 assert.deepEqual(await boundedJSON('http://fixture/__observations',{},async(_,options)=>{init=options;return reply([])}),[])
 assert.equal(init.cache,'no-store')
 await assert.rejects(()=>boundedJSON('http://fixture/__observations',{},()=>new Promise(()=>{}),15),/deadline/)
})

test('live response contract rejects stale/cache-replayed identity and mismatched snapshots',()=>{
 const state=createFixtureState(),ack=state.control({status:429}),{row}=state.select(req())
 const valid={name:'pure response correlation',ack,keyTag:'A',before:[],after:[row],actual:{status:429,id:row.responseId,body:{error:{message:'fixture usage response'},fixture:{revision:row.revision,sequence:row.sequence,status:row.status,responseId:row.responseId,keyTag:row.keyTag}}}}
 assert.doesNotThrow(()=>assertFresh(valid))
 for(const mutate of [x=>x.after=[],x=>x.after.push(x.after[0]),x=>x.actual.id='d0-r0-s0',x=>x.actual.status=403,x=>x.after[0].revision++,x=>x.after[0].sequence++,x=>x.actual.body.fixture.keyTag='B',x=>x.after[0].headers.cookie='PANEL']){
  const invalid=structuredClone(valid);mutate(invalid);assert.throws(()=>assertFresh(invalid))
 }
})

// Invoke the real HTTP handler with in-memory streams; never listen/connect.
const invoke=(server,url,body='')=>new Promise((resolve,reject)=>{
 const request=Readable.from(body?[body]:[])
 Object.assign(request,{method:url==='/__control'?'POST':'GET',url,headers:{authorization:'Bearer API-KEY-SENTINEL'},rawHeaders:['Authorization','Bearer API-KEY-SENTINEL']})
 const response={statusCode:200,headers:{},setHeader(name,value){this.headers[name.toLowerCase()]=value},end(value=''){resolve({status:this.statusCode,headers:this.headers,body:String(value)})}}
 Promise.resolve(server.listeners('request')[0](request,response)).catch(reject)
})
test('real fixture handler confirms applied control, no-store observations, and cacheable unique responses without a listener',async()=>{
 const server=createFixtureServer();assert.equal(server.listening,false)
 const control=await invoke(server,'/__control',JSON.stringify({status:403}))
 assert.equal(control.status,200);assert.equal(control.headers['cache-control'],'no-store')
 const ack=JSON.parse(control.body);assert.equal(ack.status,403)
 const first=await invoke(server,'/v1/usage'),second=await invoke(server,'/v1/usage')
 assert.equal(first.status,403);assert.equal(first.headers['cache-control'],'public, max-age=3600')
 assert.equal(first.headers['x-request-id'],`d0-r${ack.revision}-s1`)
 assert.equal(second.headers['x-request-id'],`d0-r${ack.revision}-s2`)
 assert.equal(JSON.parse(first.body).fixture.responseId,first.headers['x-request-id'])
 const observations=await invoke(server,'/__observations')
 assert.equal(observations.headers['cache-control'],'no-store');assert.equal(JSON.parse(observations.body).length,2)
 assert.equal(server.listening,false)
})
test('real fixture bad control returns failure without applying partial status change',async()=>{
 const server=createFixtureServer()
 const bad=await invoke(server,'/__control',JSON.stringify({status:429,setupHang:'invalid'}))
 assert.equal(bad.status,400);assert.equal(bad.headers['cache-control'],'no-store')
 const result=await invoke(server,'/v1/usage')
 assert.equal(result.status,200);assert.equal(JSON.parse(result.body).fixture.revision,0)
 assert.equal(server.listening,false)
})
