import http from 'node:http'
import { createHash } from 'node:crypto'
const requests=[]
let setup={code:0,data:{needs_setup:false,step:'completed'}}
let status=200
let wsRedirect=false
let setupHang=''
const server=http.createServer(async(req,res)=>{
  let body='';for await(const chunk of req)body+=chunk
  const url=new URL(req.url,'http://fixture')
  if(url.pathname==='/__observations'){res.setHeader('Content-Type','application/json');return res.end(JSON.stringify(requests))}
  if(url.pathname==='/__control'){
    const v=JSON.parse(body||'{}');if(v.clear)requests.length=0;if(v.setup)setup=v.setup;if(v.status!==undefined)status=v.status;if(v.wsRedirect!==undefined)wsRedirect=v.wsRedirect
    if(v.setupHang!==undefined)setupHang=v.setupHang
    return res.end('ok')
  }
  requests.push({method:req.method,url:req.url,headers:req.headers,rawHeaders:req.rawHeaders,body})
  if(url.pathname==='/setup/status' && setupHang==='headers')return
  res.setHeader('Access-Control-Allow-Origin','*');res.setHeader('Access-Control-Allow-Credentials','true')
  res.setHeader('Vary','Accept-Encoding');res.setHeader('X-Request-Id','d0-fixture-id');res.setHeader('Cache-Control','public, max-age=3600')
  res.setHeader('Set-Cookie','gateway_fixture=must-not-be-stored; Path=/; SameSite=Lax')
  if(url.pathname.endsWith('/redirect')){res.writeHead(302,{Location:'http://127.0.0.1:8090/leak'});return res.end()}
  if(url.pathname==='/third'){res.setHeader('Content-Type','text/html');return res.end('<!doctype html><title>Third origin fixture</title>')}
  res.setHeader('Content-Type','application/json');res.statusCode=status
  if(url.pathname==='/setup/status'){
    if(setupHang==='body'){res.write('{"code":0,"data":');return}
    return res.end(JSON.stringify(setup))
  }
  if(url.pathname==='/api/v1/settings/public')return res.end(JSON.stringify({code:0,data:{site_name:'D0 fixture',registration_enabled:false}}))
  if(url.pathname==='/v1/usage')return res.end(JSON.stringify({error:{message:'fixture usage response'}}))
  res.end(JSON.stringify({code:0,data:{},id:'job',items:[],models:[]}))
})
server.on('upgrade',(req,socket)=>{
 requests.push({method:'WS',url:req.url,headers:req.headers,rawHeaders:req.rawHeaders})
 if(wsRedirect){socket.end('HTTP/1.1 302 Found\r\nLocation: ws://127.0.0.1:8090/ws-leak\r\nContent-Length: 0\r\n\r\n');return}
 const accept=createHash('sha1').update(req.headers['sec-websocket-key']+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64')
 socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: '+accept+'\r\nSec-WebSocket-Protocol: sub2api-admin\r\n\r\n')
 socket.on('error',()=>{});setTimeout(()=>socket.end(),300)
})
server.listen(8080,'0.0.0.0')
