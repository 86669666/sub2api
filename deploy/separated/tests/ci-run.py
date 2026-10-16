#!/usr/bin/env python3
"""D0 execution gate. Only an ephemeral GitHub Actions Linux job may run this."""
import argparse, datetime, hashlib, http.client, json, os, pathlib, re, subprocess, time, uuid, signal
p=argparse.ArgumentParser();p.add_argument('--execute',action='store_true');p.add_argument('--evidence',type=pathlib.Path,required=True);args=p.parse_args()
if not args.execute or os.environ.get('GITHUB_ACTIONS')!='true' or os.uname().sysname!='Linux':
    p.error('requires --execute in an authorized ephemeral GitHub Actions Linux job; no local runtime fallback')
signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
root=pathlib.Path(__file__).resolve().parents[3];out=args.evidence.resolve();out.mkdir(parents=True,exist_ok=False)
env={k:os.environ[k] for k in ('PATH','HOME') if k in os.environ}
run_id='sub2api-d0-'+uuid.uuid4().hex[:12];containers=[];images=[];network=False
receipt={'run_id':run_id,'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'HTTP loopback; mock upstream only, no DB/Redis/provider; not public TLS acceptance','commands':[],'checks':[],'outcome':'failed','teardown':[]}
def run(cmd,check=True):
    r=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    n=len(receipt['commands']);(out/f'command-{n:03}.log').write_text(r.stdout)
    receipt['commands'].append({'argv':cmd,'exit_code':r.returncode,'log':f'command-{n:03}.log'})
    if check and r.returncode:raise RuntimeError(f'command failed: {cmd[:3]} (see command-{n:03}.log)')
    return r

def request(port,path,method='GET',headers=(),body=None):
    c=http.client.HTTPConnection('127.0.0.1',port,timeout=10)
    c.putrequest(method,path)
    for k,v in headers:c.putheader(k,v)
    if body is not None:c.putheader('Content-Length',str(len(body.encode())))
    c.endheaders(body.encode() if body is not None else None)
    r=c.getresponse();data=r.read();result={'status':r.status,'headers':r.getheaders(),'body':data.decode(errors='replace')};c.close();return result

def control(**values):request(8090,'/__control','POST',body=json.dumps(values))
def rows():return json.loads(request(8090,'/__observations')['body'])
def header(result,name):return [v for k,v in result['headers'] if k.lower()==name.lower()]
def check(name,result,status,allow=False):
    assert result['status']==status,(name,result)
    assert header(result,'Access-Control-Allow-Origin')==(['http://127.0.0.1:8088'] if allow else []),(name,result)
    assert not header(result,'Access-Control-Allow-Credentials'),(name,result)
    receipt['checks'].append({'name':name,**result})

try:
    sha=run(['git','rev-parse','HEAD']).stdout.strip();assert re.fullmatch('[a-f0-9]{40}',sha)
    assert not run(['git','status','--porcelain']).stdout.strip(),'candidate must be clean'
    receipt['candidate']=sha;receipt['runner']={'os':os.uname().sysname,'arch':os.uname().machine,'github_run_id':os.environ.get('GITHUB_RUN_ID')}
    assert run(['docker','context','show']).stdout.strip()=='default','no alternate Docker context permitted'
    run(['docker','version'])
    base_images=['nginx:1.27-alpine','node:24-alpine','mcr.microsoft.com/playwright:v1.56.1-noble']
    for image in base_images:
        run(['docker','pull',image]);run(['docker','image','inspect',image])
    # Use the pulled immutable image IDs for the two production builds.
    nginx_id=json.loads(run(['docker','image','inspect','nginx:1.27-alpine']).stdout)[0]['RepoDigests'][0]
    node_id=json.loads(run(['docker','image','inspect','node:24-alpine']).stdout)[0]['RepoDigests'][0]
    browser_id=json.loads(run(['docker','image','inspect','mcr.microsoft.com/playwright:v1.56.1-noble']).stdout)[0]['RepoDigests'][0]
    common=['--build-arg','PANEL_PUBLIC_ORIGIN=http://127.0.0.1:8088','--build-arg','GATEWAY_PUBLIC_ORIGIN=http://127.0.0.1:8089','--build-arg','SOURCE_COMMIT='+sha,'--build-arg','NGINX_IMAGE='+nginx_id,'--build-arg','NODE_IMAGE='+node_id]
    for name,file,target in [('panel','Dockerfile.frontend','integration-panel'),('gateway','Dockerfile.gateway',None),('mock','tests/Dockerfile.mock',None),('browser','tests/Dockerfile.browser',None)]:
        image=run_id+'-'+name;images.append(image)
        command=['docker','build','-f','deploy/separated/'+file,'-t',image]
        if name in ('panel','gateway'):command+=common
        if name=='mock':command+=['--build-arg','NODE_IMAGE='+node_id]
        if name=='browser':command+=['--build-arg','PLAYWRIGHT_IMAGE='+browser_id]
        if target:command+=['--target',target]
        run(command+['.']);run(['docker','image','inspect',image])
    run(['docker','network','create','--internal','--label','sub2api.d0.owner='+run_id,run_id]);network=True
    for name,port in [('mock',8090),('panel',8088),('gateway',8089)]:
        cname=run_id+'-'+name;containers.append(cname)
        cmd=['docker','run','-d','--name',cname,'--label','sub2api.d0.owner='+run_id,'--network',run_id,'-p',f'127.0.0.1:{port}:'+('8080' if name=='mock' else '80')]
        if name=='mock':cmd+=['--network-alias','backend']
        run(cmd+[cname])
    for _ in range(40):
        try:
            if request(8088,'/setup/status')['status']==200:break
        except OSError:pass
        time.sleep(.5)
    else:raise RuntimeError('isolated panel did not become reachable')
    manifests=[]
    for name,path in [('panel','/usr/share/nginx/html/deployment-manifest.json'),('gateway','/etc/nginx/deployment-manifest.json')]:
        c=run_id+'-'+name
        version=run(['docker','exec',c,'nginx','-v']).stdout;assert 'nginx/1.27.' in version
        run(['docker','exec',c,'nginx','-t']);run(['docker','exec',c,'nginx','-T'])
        manifests.append(json.loads(run(['docker','exec',c,'cat',path]).stdout))
    assert manifests[0]==manifests[1] and manifests[0]['candidate']==sha
    receipt['manifest']=manifests[0]
    # Inspect production app assets, not the separately compiled test harness.
    asset_check = 'for f in /usr/share/nginx/html/assets/*.js; do if grep -Fq "$1" "$f" && grep -Fq "$2" "$f" && grep -Fq "$3" "$f"; then sha256sum "$f"; fi; done'
    asset_receipt=run(['docker','exec',run_id+'-panel','sh','-c',asset_check,'--',sha,manifests[0]['panel'],manifests[0]['gateway']]).stdout.strip()
    assert asset_receipt, 'production app bundle does not contain the matching candidate/origin contract'
    receipt['production_bundle_contract_sha256']=asset_receipt
    origin=('Origin','http://127.0.0.1:8088')
    for hs in ['', 'authorization', ' Content-Type , AUTHORIZATION, idempotency-key ', 'authorization,authorization']:
        control(clear=True)
        check('valid preflight '+hs,request(8089,'/v1/images/batches','OPTIONS',[origin,('Access-Control-Request-Method','POST'),('Access-Control-Request-Headers',hs)]),204,True);assert rows()==[]
    invalid=[[( 'Origin',x),('Access-Control-Request-Method','GET')] for x in ['null','http://127.0.0.1:8090','HTTP://127.0.0.1:8088','http://127.0.0.1:8088.evil','http://127.0.0.1:8088, null']]
    invalid += [[origin], [origin,('Access-Control-Request-Method','get')], [origin,('Access-Control-Request-Method','PUT')], [],[origin,origin,('Access-Control-Request-Method','GET')]]
    invalid += [[origin,('Access-Control-Request-Method','POST'),('Access-Control-Request-Headers',x)] for x in ['authorization,','authorization,,content-type','x-api-key','cookie','authorization;content-type']]
    for hs in invalid:
        control(clear=True);check('invalid preflight',request(8089,'/v1/usage','OPTIONS',hs),403);assert rows()==[]
    for path in ['/v1/images/batchesX','/v1/USAGE','/v1/images/%62atches','/v1/images/batches/%2e','/v1/images/batches/a%2fb','/v1/images/batches/%252f','/v1//usage','/v1/images/../usage']:
        control(clear=True);check('rejected path '+path,request(8089,path,headers=[origin]),403);assert rows()==[]
    for status in [401,403,429,500]:
        control(status=status);r=request(8089,'/v1/usage',headers=[origin,('Authorization','Bearer FIXTURE')]);check('actual backend error',r,status,True);assert 'Accept-Encoding' in ','.join(header(r,'Vary'))
    control(status=200)
    check('actual method rejected',request(8089,'/v1/usage','PUT',[origin]),405,True)
    check('SDK unchanged',request(8089,'/v1/other',headers=[('Authorization','Bearer FIXTURE')]),200)
    check('health strips upstream CORS',request(8089,'/health'),200)
    for method in ['HEAD','POST','OPTIONS','PUT','DELETE','PATCH']:
        control(clear=True);r=request(8088,'/setup/status',method);assert r['status']==405 and header(r,'Allow')==['GET'] and header(r,'Cache-Control')==['no-store'];assert rows()==[];receipt['checks'].append({'name':'status '+method,**r})
    control(clear=True)
    r=request(8088,'/setup/status?q=1',headers=[('Authorization','Bearer PANEL'),('Cookie','panel=SENTINEL')]);assert r['status']==200 and header(r,'Cache-Control')==['no-store']
    assert rows()[0]['url']=='/setup/status?q=1' and 'authorization' not in rows()[0]['headers'] and 'cookie' not in rows()[0]['headers']
    for path in ['/setup','/setup/status/','/setup/install','/setup/test-db','/setup/test-redis']:
        control(clear=True);assert request(8088,path)['status']==404;assert rows()==[];assert request(8089,path)['status']==404
    for path in ['/setup%2fstatus','/setup//status','/setup/./status']:
        assert request(8088,path)['status']==400
    for path in ['/','/some-spa-path','/assets/missing.js']:
        r=request(8088,path);csp=header(r,'Content-Security-Policy');assert len(csp)==1 and "connect-src 'self' http://127.0.0.1:8089 ws://127.0.0.1:8088;" in csp[0]
    assert request(8088,'/v1/usage')['status']==404 and request(8089,'/api/v1/auth/me')['status']==404
    # Actual browser container shares only the CI host loopback namespace.
    browser=run_id+'-browser-run';containers.append(browser)
    run(['docker','run','--name',browser,'--network','host','--label','sub2api.d0.owner='+run_id,'-v',str(out)+':/evidence',run_id+'-browser'])
    run(['docker','stop',run_id+'-mock'])
    check('edge gateway 502',request(8089,'/v1/usage',headers=[origin]),502,True)
    r=request(8088,'/setup/status');assert r['status']==502 and header(r,'Cache-Control')==['no-store']
    receipt['outcome']='passed'
finally:
    for c in reversed(containers):
        run(['docker','logs',c],check=False)
        owner=run(['docker','inspect','--format','{{ index .Config.Labels "sub2api.d0.owner" }}',c],check=False)
        if owner.returncode:
            receipt['teardown'].append({'container':c,'exit_code':0,'state':'not present'})
        elif owner.stdout.strip()==run_id:
            r=run(['docker','rm','-f',c],check=False);receipt['teardown'].append({'container':c,'exit_code':r.returncode})
        else:
            receipt['teardown'].append({'container':c,'exit_code':1,'state':'ownership mismatch; not touched'})
    if network:
        r=run(['docker','network','rm',run_id],check=False);receipt['teardown'].append({'network':run_id,'exit_code':r.returncode})
    for image in images:run(['docker','image','rm',image],check=False)
    receipt['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    if any(x['exit_code'] for x in receipt['teardown']):receipt['outcome']='failed_teardown'
    (out/'receipt.json').write_text(json.dumps(receipt,indent=2))
    manifest={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in out.iterdir() if p.is_file()}
    (out/'manifest.json').write_text(json.dumps(manifest,indent=2))
if receipt['outcome']!='passed':raise SystemExit(1)
