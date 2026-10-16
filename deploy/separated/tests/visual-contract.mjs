import assert from 'node:assert/strict'
export function assertChartSnapshot(s) {
 assert.ok(s.width>0 && s.height>0 && s.ink>100,'canvas must actually contain painted pixels')
 assert.equal(s.datasets.length,5)
 const [input,,creation,read,hit]=s.datasets
 assert.notDeepEqual(creation.dash,read.dash,'cache curves must have distinct strokes')
 assert.notEqual(creation.point,read.point,'cache points must remain distinguishable')
 const legends=s.legend.filter(x=>['Cache Creation','Cache Read'].includes(x.text))
 assert.equal(legends.length,2);assert.notEqual(legends[0].point,legends[1].point)
 assert.ok(s.legend.every(x=>x.box.width>0 && x.box.height>0),'legend must have rendered geometry')
 assert.ok(s.chartArea.bottom>s.chartArea.top && s.chartArea.right>s.chartArea.left,'legend must not consume entire plot')
 assert.ok(input.points.every(p=>Number.isFinite(p.x)&&Number.isFinite(p.y)&&p.ink>0),'actual data points must be painted')
 assert.deepEqual(hit.data,input.data.map((n,i)=>{const total=n+creation.data[i]+read.data[i];return total?read.data[i]/total*100:0}))
}
export function assertThemeTransition(before,after) {
 assertChartSnapshot(before);assertChartSnapshot(after)
 assert.equal(after.id,before.id,'Chart instance must survive theme switch')
 assert.equal(after.sameCanvas,true,'same mounted canvas required')
 assert.deepEqual(after.datasets.map(x=>x.data),before.datasets.map(x=>x.data))
 for(const key of ['line','fill','point','muted','faint','hitRate','legend','xGrid','yGrid','xText','yText','percentText'])assert.notEqual(after.colors[key],before.colors[key],key+' must update')
 assert.notEqual(after.pixelHash,before.pixelHash,'Canvas pixels must be redrawn')
}
export function assertTableSnapshot(s,{virtual,atEnd=false}={}) {
 assert.equal(s.virtual,virtual,'default 100/101 virtualization boundary')
 assert.ok(s.rows.length>0 && s.visible.length>0,'no blank table/viewport')
 assert.equal(new Set(s.rows.map(r=>r.id)).size,s.rows.length,'no duplicate rendered rows')
 if(virtual)assert.ok(s.rows.length<s.ids.length,'large dataset must be windowed')
 else assert.equal(s.rows.length,s.ids.length,'small dataset must render fully')
 for(let i=0;i<s.rows.length;i++){
  const row=s.rows[i];assert.equal(row.id,String(s.ids[row.index]),'row identity must match sorted/filter/page data')
  assert.ok(row.height>0)
  if(i){assert.equal(row.index,s.rows[i-1].index+1,'no skipped rows');assert.ok(row.top>=s.rows[i-1].bottom-1,'rows must not overlap')}
 }
 const first=s.visible[0],last=s.visible.at(-1)
 assert.ok(first.top<=s.contentTop+2,'no blank strip at viewport start')
 assert.ok(last.bottom>=(s.rows.at(-1).index===s.ids.length-1?Math.min(s.bottom,s.lastDataBottom):s.bottom)-2,'no blank strip inside visible data')
 if(atEnd){assert.equal(s.rows.at(-1).id,String(s.ids.at(-1)),'last data row reachable');assert.ok(last.bottom<=s.bottom+2)}
 assert.ok(s.scrollHeight>=s.clientHeight)
}
export function assertStableScroll(samples) {
 assert.ok(samples.length>=3)
 assert.ok(Math.max(...samples)-Math.min(...samples)<=2,'scroll must stop drifting after settling')
}
export const viewports=[320,768,1440]
export const pageCases=[
 {name:'login',path:'/login',role:null,ready:'form'},
 {name:'home',path:'/home',role:null,ready:'h1'},
 {name:'key-usage',path:'/key-usage',role:null,ready:'input[type=password]'},
 {name:'user-dashboard',path:'/dashboard',role:'user',ready:'main .card',api:'/api/v1/usage/dashboard/stats'},
 {name:'admin-dashboard',path:'/admin/dashboard',role:'admin',ready:'main .card',api:'/api/v1/admin/dashboard/snapshot-v2'},
 {name:'settings',path:'/admin/settings',role:'admin',ready:'main input',api:'/api/v1/admin/settings'},
 {name:'ops',path:'/admin/ops',role:'admin',ready:'main',api:'/api/v1/admin/ops/dashboard/snapshot-v2'},
]
