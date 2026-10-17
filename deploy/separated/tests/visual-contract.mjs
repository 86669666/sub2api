import assert from 'node:assert/strict'
// Compare actual getImageData samples, composited over the page's CSS backdrop.
// Alpha alone cannot separate a cache stroke from Input fill or a grid line.
export function cacheRasterEvidence(r) {
 assert.equal(r?.method,'same-canvas-controller-draw-ablation-v1')
 assert.equal(r.restored,true,'draw methods and original pixels must be restored')
 assert.ok(Array.isArray(r.backdrop)&&r.backdrop.length===3&&r.backdrop.every(v=>Number.isFinite(v)&&v>=0&&v<=255))
 const names=['Input','Output','Cache Creation','Cache Read','Cache Hit Rate']
 const trace=(actual,indices)=>assert.deepEqual(actual,[...indices].reverse().map(index=>({index,label:names[index]})),'actual controller draw attribution')
 trace(r.fullDraws,[0,1,2,3,4]);trace(r.baselineDraws,[])
 assert.equal(r.targets?.length,2)
 const rgb=(bytes,n)=>r.backdrop.map((b,k)=>bytes[n+k]*bytes[n+3]/255+b*(1-bytes[n+3]/255))
 const distance=(a,b)=>Math.max(...a.map((v,k)=>Math.abs(v-b[k])))
 const results=r.targets.map((target,t)=>{
  const index=t+2;assert.equal(target.index,index);assert.equal(target.label,names[index])
  trace(target.isolatedDraws,[index]);trace(target.withoutDraws,[0,1,2,3,4].filter(i=>i!==index))
  assert.ok(target.patches?.length>=1,'cache point samples required')
  const patches=target.patches.map((p,pointIndex)=>{
   assert.equal(p.pointIndex,pointIndex);assert.ok(Number.isFinite(p.x)&&Number.isFinite(p.y))
   assert.equal(p.width,13);assert.equal(p.height,13)
   for(const key of ['baseline','isolated','full','without'])assert.ok(Array.isArray(p[key])&&p[key].length===13*13*4&&p[key].every(v=>Number.isInteger(v)&&v>=0&&v<=255),'complete RGBA samples required: '+key)
   const mask=[],visible=[]
   for(let n=0;n<p.full.length;n+=4){
    const isolated=rgb(p.isolated,n),full=rgb(p.full,n)
    // A no-cache baseline removes fixed grid/fill contamination. Requiring
    // contrast against CSS too rejects background-colored erasure of grid ink.
    if(distance(isolated,rgb(p.baseline,n))>=16&&distance(isolated,r.backdrop)>=16){
     mask.push(n/4)
     if(distance(full,rgb(p.without,n))>=12&&distance(full,r.backdrop)>=16)visible.push(n/4)
    }
   }
   return {mask,visible}
  })
  const pixels=patches.reduce((n,p)=>n+p.mask.length,0),visiblePixels=patches.reduce((n,p)=>n+p.visible.length,0)
  assert.ok(pixels>=3,'cache dataset must paint contrasting pixels: '+target.label)
  assert.ok(visiblePixels>=2,'cache pixels must contribute to the original composite: '+target.label)
  return {index,label:target.label,pixels,visiblePixels,patches}
 })
 assert.equal(results[0].patches.length,results[1].patches.length)
 // Compare normalized marker neighborhoods, not style strings or global ink.
 const exclusive=[0,0]
 results[0].patches.forEach((p,i)=>{
  const a=new Set(p.mask),b=new Set(results[1].patches[i].mask)
  exclusive[0]+=[...a].filter(x=>!b.has(x)).length;exclusive[1]+=[...b].filter(x=>!a.has(x)).length
 })
 assert.ok(exclusive.every(n=>n>=2),'cache raster signatures must be distinguishable')
 return {targets:results.map(({index,label,pixels,visiblePixels})=>({index,label,pixels,visiblePixels})),exclusive}
}
export function assertTrendRegion(s,state) {
 assert.equal(s?.response?.path,'/api/v1/usage/dashboard/trend')
 assert.equal(s.response.method,'GET');assert.equal(s.response.status,200);assert.equal(s.response.body?.code,0)
 const trend=s.response.body?.data?.trend;assert.ok(Array.isArray(trend),'actual trend response required')
 assert.equal(s.regionCount,1);assert.equal(s.heading,'Token Usage Trend');assert.equal(s.visible,true)
 assert.equal(s.loading,false,'trend loading must finish')
 if(state==='empty'){
  assert.deepEqual(trend,[],'normal response cannot satisfy empty state')
  assert.equal(s.emptyText,'No data available');assert.equal(s.canvasCount,0,'old trend canvas must be removed')
  assert.equal(s.previousCanvasConnected,false,'normal canvas must not survive the transition')
 } else {
  assert.equal(state,'normal');assert.ok(trend.length>0);assert.equal(s.emptyText,null)
  assert.equal(s.canvasCount,1);assert.ok(s.canvasInk>100,'normal trend needs a painted positive control')
 }
}
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
 cacheRasterEvidence(s.cacheRaster)
 for(const index of [2,3]){
  const dataset=s.datasets[index],target=s.cacheRaster.targets[index-2]
  assert.equal(dataset.label,target.label)
  assert.equal(dataset.points?.length,dataset.data.length)
  assert.ok(dataset.points.every(p=>Number.isFinite(p.x)&&Number.isFinite(p.y)&&p.ink>0),'cache point coordinates and ink required in original snapshot')
  assert.deepEqual(target.patches.map(p=>({x:p.x,y:p.y})),dataset.points.map(p=>({x:p.x,y:p.y})))
 }
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
