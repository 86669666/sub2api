import http from 'node:http'
import { createHash } from 'node:crypto'
import { pathToFileURL } from 'node:url'

// Importable without opening a listener: pure tests exercise the actual fixture state.
export function createFixtureState(owner = 'pure-test') {
 const requests=[]
 let revision=0, sequence=0, status=200, wsRedirect=false, setupHang='', faultMode='none'
 let setup={code:0,data:{needs_setup:false,step:'completed'}}
 return {
  requests,
  control(value) {
   if(!value || typeof value!=='object' || Array.isArray(value))throw new Error('invalid control')
   if(value.status!==undefined && (!Number.isInteger(value.status) || value.status<200 || value.status>599))throw new Error('invalid status')
   if(value.setupHang!==undefined && !['','headers','body'].includes(value.setupHang))throw new Error('invalid setupHang')
   if(value.wsRedirect!==undefined && typeof value.wsRedirect!=='boolean')throw new Error('invalid wsRedirect')
   if(value.clear!==undefined && typeof value.clear!=='boolean')throw new Error('invalid clear')
   if(value.setup!==undefined && (!value.setup || typeof value.setup!=='object' || Array.isArray(value.setup)))throw new Error('invalid setup')
   if(value.faultMode!==undefined && !['none','close-before-headers'].includes(value.faultMode))throw new Error('invalid faultMode')
   const previousRevision=revision
   if(value.clear)requests.length=0
   if(value.status!==undefined)status=value.status
   if(value.setup!==undefined)setup=structuredClone(value.setup)
   if(value.wsRedirect!==undefined)wsRedirect=value.wsRedirect
   if(value.setupHang!==undefined)setupHang=value.setupHang
   if(value.faultMode!==undefined)faultMode=value.faultMode
   revision++
   return {owner,previousRevision,revision,status,sequence,setupHang,wsRedirect,faultMode,setup:structuredClone(setup)}
  },
  select(req,body='',upgrade=false) {
   const path=new URL(req.url,'http://fixture').pathname
   const selectedStatus=upgrade?(wsRedirect?302:101):(path.endsWith('/redirect')?302:(path==='/third'?200:status))
   const authorization=req.headers.authorization
   const keyTag=authorization==='Bearer API-KEY-SENTINEL'?'A':authorization==='Bearer API-KEY-B-SENTINEL'?'B':authorization?'other':'none'
   const row={sequence:++sequence,revision,status:selectedStatus,responseId:`d0-r${revision}-s${sequence}`,keyTag,
    method:upgrade?'WS':req.method,url:req.url,headers:{...req.headers},rawHeaders:[...req.rawHeaders],body}
   requests.push(row)
   // A response uses this snapshot even if a subsequent control changes global state.
   return {row,faultMode,owner,setup:structuredClone(setup),setupHang,wsRedirect}
  },
 }
}

// This helper is the actual handler path, also exercised with in-memory sockets.
export function closeBeforeHeaders(req,res,selected) {
 const {row,faultMode,owner}=selected
 if(faultMode!=='close-before-headers' || req.method!=='GET' || !['/v1/usage','/setup/status'].includes(req.url))return false
 if(res.headersSent)throw new Error('fault must precede headers')
 const socket=req.socket, start=socket.bytesWritten
 row.configuredStatus=row.status;row.status=null
 row.fault={owner,mode:faultMode,headersSent:false,httpStatus:null,responseBytes:0,
  socketId:`${socket.localAddress}:${socket.localPort}<-${socket.remoteAddress}:${socket.remotePort}`,
  localAddress:socket.localAddress,localPort:socket.localPort,remoteAddress:socket.remoteAddress,remotePort:socket.remotePort,
  requestedAt:new Date().toISOString(),closeRequested:true,closeCompleted:false}
 socket.once('close',()=>{
  row.fault.closeCompleted=true;row.fault.closedAt=new Date().toISOString()
  row.fault.headersSent=res.headersSent;row.fault.responseBytes=socket.bytesWritten-start
 })
 socket.destroy()
 return true
}
export function createFixtureServer(owner = 'pure-test') {
 const state=createFixtureState(owner)
 const server=http.createServer(async(req,res)=>{
  let body='';for await(const chunk of req)body+=chunk
  const url=new URL(req.url,'http://fixture')
  if(url.pathname==='/__observations' || url.pathname==='/__control'){
   res.setHeader('Content-Type','application/json');res.setHeader('Cache-Control','no-store')
   if(url.pathname==='/__observations')return res.end(JSON.stringify(state.requests))
   if(req.method!=='POST'){res.statusCode=405;return res.end(JSON.stringify({error:'control requires POST'}))}
   try{return res.end(JSON.stringify(state.control(JSON.parse(body))))}
   catch(error){res.statusCode=400;return res.end(JSON.stringify({error:error.message}))}
  }
  const selected=state.select(req,body)
  if(closeBeforeHeaders(req,res,selected))return
  const {row,setup,setupHang}=selected
  if(url.pathname==='/setup/status' && setupHang==='headers')return
  res.setHeader('Access-Control-Allow-Origin','*');res.setHeader('Access-Control-Allow-Credentials','true')
  res.setHeader('Vary','Accept-Encoding');res.setHeader('X-Request-Id',row.responseId);res.setHeader('Cache-Control','public, max-age=3600')
  res.setHeader('Set-Cookie','gateway_fixture=must-not-be-stored; Path=/; SameSite=Lax')
  res.statusCode=row.status
  if(url.pathname.endsWith('/redirect')){res.setHeader('Location','http://127.0.0.1:8090/leak');return res.end()}
  if(url.pathname==='/third'){res.setHeader('Content-Type','text/html');return res.end('<!doctype html><title>Third origin fixture</title>')}
  res.setHeader('Content-Type','application/json')
  if(url.pathname==='/setup/status'){
   if(setupHang==='body'){res.write('{"code":0,"data":');return}
   return res.end(JSON.stringify(setup))
  }
  if(url.pathname==='/api/v1/settings/public')return res.end(JSON.stringify({code:0,data:{site_name:'D0 fixture',registration_enabled:false}}))
  const identity={revision:row.revision,sequence:row.sequence,status:row.status,responseId:row.responseId,keyTag:row.keyTag}
  if(url.pathname==='/v1/usage')return res.end(JSON.stringify({error:{message:'fixture usage response'},fixture:identity}))
  res.end(JSON.stringify({code:0,data:{},id:'job',items:[],models:[],fixture:identity}))
 })
 server.on('upgrade',(req,socket)=>{
  const {wsRedirect}=state.select(req,'',true)
  if(wsRedirect){socket.end('HTTP/1.1 302 Found\r\nLocation: ws://127.0.0.1:8090/ws-leak\r\nContent-Length: 0\r\n\r\n');return}
  const accept=createHash('sha1').update(req.headers['sec-websocket-key']+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64')
  socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: '+accept+'\r\nSec-WebSocket-Protocol: sub2api-admin\r\n\r\n')
  socket.on('error',()=>{});setTimeout(()=>socket.end(),300)
 })
 return server
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href)createFixtureServer(process.env.D0_OWNER).listen(8080,'0.0.0.0')
