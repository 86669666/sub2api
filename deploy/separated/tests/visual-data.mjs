// CI-only deterministic inputs. These are never imported by the production app.
export const visualModes = ['off', 'normal', 'empty', 'error', 'loading']
export function trendFixture(kind = 'crossing') {
 const pairs = kind === 'zero' ? [[0,0],[0,0],[0,0]] : kind === 'overlap' ? [[50,51],[51,50],[50,50]] : [[10,90],[90,10],[50,50]]
 return pairs.map(([creation,read],i)=>({date:`2026-05-0${i+1}`,requests:1,input_tokens:kind==='zero'?0:20,output_tokens:kind==='zero'?0:5,cache_creation_tokens:creation,cache_read_tokens:read,cost:0.02,actual_cost:0.01}))
}
export const publicSettings = {site_name:'Fixture API · long workspace name',site_logo:'',registration_enabled:true,password_reset_enabled:true,turnstile_enabled:false,login_agreement_enabled:false,backend_mode_enabled:false,ops_monitoring_enabled:true,table_page_size_options:[10,20,50,100],custom_menu_items:[],home_content:'',doc_url:''}
const user = role => ({id:role==='admin'?101:102,email:`${role}@fixture.invalid`,username:'Long fixture display name for responsive layout',role,status:'active',balance:1234567.89,concurrency:3,run_mode:'standard',created_at:'2026-05-01T00:00:00Z',updated_at:'2026-05-01T00:00:00Z',allowed_groups:[],subscriptions:[]})
const stats = Object.fromEntries(['total_users','today_new_users','active_users','hourly_active_users','total_api_keys','active_api_keys','total_accounts','normal_accounts','error_accounts','ratelimit_accounts','overload_accounts','total_requests','total_input_tokens','total_output_tokens','total_cache_creation_tokens','total_cache_read_tokens','total_tokens','today_requests','today_input_tokens','today_output_tokens','today_cache_creation_tokens','today_cache_read_tokens','today_tokens','average_duration_ms','uptime','rpm','tpm'].map(k=>[k,12]))
Object.assign(stats,{total_cost:1234567.89,total_actual_cost:123456.78,today_cost:9876.54,today_actual_cost:8765.43,stats_updated_at:'2026-05-01T00:00:00Z',stats_stale:false})
const overview={start_time:'2026-05-01T00:00:00Z',end_time:'2026-05-01T01:00:00Z',platform:'',health_score:100,success_count:12,error_count_total:0,business_limited_count:0,error_count_sla:0,request_count_total:12,request_count_sla:12,token_consumed:1234,sla:100,error_rate:0,upstream_error_rate:0,upstream_error_count_excl_429_529:0,upstream_429_count:0,upstream_529_count:0,qps:{current:1,peak:2,avg:1},tps:{current:12,peak:24,avg:12},duration:{p50_ms:100,p95_ms:200,avg_ms:110},ttft:{p50_ms:20,p95_ms:50,avg_ms:25},job_heartbeats:[],system_metrics:null}
const throughput={bucket:'minute',points:[],by_platform:[],top_groups:[]}
const errorTrend={bucket:'minute',points:[]}
const auxiliarySettings = {
 '/admin/settings/admin-api-key':{exists:false},
 '/admin/settings/overload-cooldown':{enabled:false,cooldown_minutes:1},
 '/admin/settings/rate-limit-429-cooldown':{enabled:false,cooldown_minutes:1},
 '/admin/settings/panel-rate-limit':{enabled:true,user_rpm:240,heavy_rpm:60,exempt_admin:true,public_ip_rpm:300},
 '/admin/settings/stream-timeout':{enabled:false,timeout_seconds:300},
 '/admin/settings/rectifier':{enabled:false},
 '/admin/settings/beta-policy':{rules:[],default_action:'pass'},
 '/admin/accounts/upstream-billing-probe/settings':{enabled:false,interval_minutes:30},
 '/admin/accounts/ollama-cloud-usage/settings':{enabled:false,interval_minutes:60,debounce_minutes:1},
 '/admin/accounts/opencode-go-usage/settings':{enabled:false,interval_minutes:60},
 '/admin/ops/advanced-settings':{display_alert_events:false,display_openai_token_stats:false,auto_refresh_enabled:false,auto_refresh_interval_seconds:30},
 '/admin/ops/settings/metric-thresholds':{},
}
// Pure response selection; server and offline tests use the same function.
// Fixture credentials go through the actual Login form/API/store/router. Unknown
// panel routes fail closed instead of silently returning successful empty data.
export function visualResponse({url,method='GET',headers={},body=''}, mode) {
 if(mode==='off')return null
 const path=new URL(url,'http://fixture').pathname
 const ok=data=>({status:200,body:{code:0,data},delay:mode==='loading'?1800:0})
 const fail=(status,message)=>({status,body:{code:status,message},delay:0})
 if(path==='/setup/status')return ok({needs_setup:false,step:'completed'})
 if(path==='/api/v1/settings/public')return ok(publicSettings)
 if(path==='/api/v1/auth/login' && method==='POST'){
  let credentials;try{credentials=JSON.parse(body)}catch{return fail(400,'Invalid fixture login')}
  const role=credentials.email==='admin@fixture.invalid'?'admin':credentials.email==='user@fixture.invalid'?'user':null
  if(!role || credentials.password!=='fixture-password')return fail(401,'Fixture login rejected')
  return ok({access_token:`VISUAL-${role}-TOKEN`,token_type:'Bearer',expires_in:3600,user:user(role)})
 }
 if(path==='/v1/usage'){
  if(headers.authorization!=='Bearer API-KEY-SENTINEL')return fail(401,'Explicit fixture API key required')
  if(mode==='error')return fail(429,'Fixture usage error')
  return {status:200,delay:mode==='loading'?1800:0,body:{type:'balance',status:'active',balance:1234567.89,usage:{today:{requests:12,input_tokens:120,output_tokens:30,total_tokens:150,actual_cost:9876.54},total:{requests:120,input_tokens:1200,output_tokens:300,total_tokens:1500,actual_cost:123456.78},rpm:1,tpm:10,average_duration_ms:100},model_stats:[],daily_usage:[]}}
 }
 if(!path.startsWith('/api/v1/'))return null
 const role=headers.authorization==='Bearer VISUAL-admin-TOKEN'?'admin':headers.authorization==='Bearer VISUAL-user-TOKEN'?'user':null
 if(!role)return fail(401,'Fixture panel authentication required')
 const p=path.slice('/api/v1'.length)
 if(p.startsWith('/admin/') && role!=='admin')return fail(403,'Fixture admin role required')
 if(p==='/auth/me' || p==='/user/profile')return ok(user(role))
 if(method!=='GET')return fail(405,'Visual fixture mutations disabled')
 if(mode==='error' && (p.includes('/dashboard/') || p==='/admin/settings'))return fail(500,'Fixture dashboard/settings error')
 const trend=mode==='empty'?[]:trendFixture()
 const map={
  '/announcements':[], '/subscriptions':[], '/subscriptions/active':[], '/subscriptions/progress':[],
  '/keys':{items:[],total:0,page:1,page_size:100,pages:0},
  '/user/platform-quotas':{platform_quotas:[]}, '/usage':{items:[],total:0,page:1,page_size:20,pages:0},
  '/usage/dashboard/stats':stats, '/usage/dashboard/trend':{trend}, '/usage/dashboard/models':{models:[]},
  '/admin/dashboard/snapshot-v2':{stats,trend,models:[],groups:[]},
  '/admin/dashboard/users-trend':{trend:[],users:[]}, '/admin/dashboard/users-ranking':{ranking:[],total:0},
  '/admin/settings':{...publicSettings,default_subscriptions:[],registration_email_suffix_whitelist:[],forwarded_client_ip_headers:[],payment_recharge_bonus_tiers:[],custom_menu_items:[]},
  '/admin/payment/config':{enabled:false}, '/admin/ops/realtime-traffic':{enabled:true,summary:{window:'1m',...overview}},
  '/admin/groups/all':[], '/admin/payment/providers':[], '/admin/proxies':{items:[],total:0},
  '/admin/ops/dashboard/snapshot-v2':{generated_at:'2026-05-01T01:00:00Z',overview,throughput_trend:throughput,error_trend:errorTrend},
  '/admin/ops/dashboard/overview':overview, '/admin/ops/dashboard/throughput-trend':throughput,
  '/admin/ops/dashboard/latency-histogram':{buckets:[],total:0}, '/admin/ops/dashboard/error-trend':errorTrend,
  '/admin/ops/dashboard/error-distribution':{total:0,items:[]},
  ...auxiliarySettings,
 }
 if(Object.hasOwn(map,p))return ok(map[p])
 return {...fail(501,'Unmapped visual API: '+p),unmapped:true}
}
