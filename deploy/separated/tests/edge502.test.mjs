import test from 'node:test'
import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { Readable } from 'node:stream'
import { createFixtureState, createFixtureServer, closeBeforeHeaders } from './mock-upstream.mjs'

const req = (url='/v1/usage',method='GET') => ({url,method,headers:{},rawHeaders:[]})
function memorySocket() {
 const socket=new EventEmitter()
 Object.assign(socket,{localAddress:'172.18.0.2',localPort:8080,remoteAddress:'172.18.0.3',remotePort:43210,bytesWritten:0,destroys:0,
  destroy(){this.destroys++;this.emit('close')}})
 return socket
}
test('fault control validates atomically, retains owner and snapshot, and disarms',()=>{
 const state=createFixtureState('owned-run'),ack=state.control({faultMode:'close-before-headers'})
 assert.equal(ack.owner,'owned-run');assert.equal(ack.faultMode,'close-before-headers')
 const selected=state.select(req())
 assert.throws(()=>state.control({status:502,clear:true,faultMode:'invalid'}))
 assert.equal(state.requests.length,1)
 const next=state.control({});assert.equal(next.status,200);assert.equal(next.revision,2)
 assert.equal(state.control({faultMode:'none'}).faultMode,'none')
 assert.equal(selected.faultMode,'close-before-headers')
})
for(const path of ['/v1/usage','/setup/status'])test('real handler closes before any headers: '+path,async()=>{
 const server=createFixtureServer('owned-run'),handler=server.listeners('request')[0]
 async function invoke(url,body='') {
  const request=Readable.from(body?[body]:[]),socket=memorySocket()
  Object.assign(request,req(url,body?'POST':'GET'),{socket})
  const response={headersSent:false,statusCode:200,body:'',headers:{},writes:0,
   setHeader(k,v){this.writes++;this.headers[k]=v},end(v=''){this.writes++;this.body=v;this.headersSent=true}}
  await handler(request,response);return {response,socket}
 }
 const control=await invoke('/__control',JSON.stringify({faultMode:'close-before-headers'}))
 assert.equal(JSON.parse(control.response.body).faultMode,'close-before-headers')
 const fault=await invoke(path)
 assert.equal(fault.response.writes,0);assert.equal(fault.socket.destroys,1)
 const observations=await invoke('/__observations'),rows=JSON.parse(observations.response.body)
 assert.equal(rows.length,1);assert.equal(rows[0].status,null);assert.equal(rows[0].configuredStatus,200)
 assert.equal(rows[0].fault.headersSent,false);assert.equal(rows[0].fault.responseBytes,0);assert.equal(rows[0].fault.closeCompleted,true)
 assert.equal(rows[0].fault.owner,'owned-run');assert.equal(server.listening,false)
 await invoke('/__control',JSON.stringify({faultMode:'none'}))
 const recovered=await invoke(path);assert.equal(recovered.socket.destroys,0);assert.equal(recovered.response.statusCode,200);assert.ok(recovered.response.writes>0)
})
test('fault exact path/method and no-headers guard reject false fault proof',()=>{
 const state=createFixtureState();state.control({faultMode:'close-before-headers'})
 for(const [path,method] of [['/__control','POST'],['/__observations','GET'],['/v1/usage','POST'],['/v1/usage?q=1','GET'],['/setup/status/','GET'],['/health','GET']]){
  const request={...req(path,method),socket:memorySocket()}
  assert.equal(closeBeforeHeaders(request,{headersSent:false},state.select(request)),false)
  assert.equal(request.socket.destroys,0)
 }
 const request={...req(),socket:memorySocket()}
 assert.throws(()=>closeBeforeHeaders(request,{headersSent:true},state.select(request)))
 assert.equal(request.socket.destroys,0)
})
test('closure evidence requires real close event and detects bytes written',()=>{
 const state=createFixtureState();state.control({faultMode:'close-before-headers'})
 const socket=memorySocket();socket.destroy=function(){this.destroys++}
 const request={...req(),socket},selected=state.select(request)
 closeBeforeHeaders(request,{headersSent:false},selected)
 assert.equal(selected.row.fault.closeCompleted,false)
 socket.bytesWritten=42;socket.emit('close');assert.equal(selected.row.fault.responseBytes,42)
})
