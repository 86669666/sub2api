import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { assertChartSnapshot, assertThemeTransition, assertTableSnapshot, assertStableScroll, viewports, pageCases } from './visual-contract.mjs'

// Called only inside the existing ephemeral D0 browser boundary. No browser is
// launched here, no routing interception, and no additional destination is allowed.
export async function runVisualAcceptance(browser,{panel,gateway,control,observed,contract}) {
 const evidence={candidate:contract.candidate,browser:browser.version(),scope:'CI-only component fixtures and actual production App with authenticated mock APIs',charts:[],tables:[],pages:[],screenshots:[],network:[],errors:[],outcome:'failed',limitations:['Chromium only; no production authentication/provider/DB/Redis','DataTable filtering and pagination belong to the fixture parent; action menu is a slot; no public DataTable internal actionsExpanded toggle exists','Onboarding checks first-step placement/navigation/close, not every guided workflow']}
 const contexts=[]
 let cleanupError
 await mkdir('/evidence/visual',{recursive:true})
 const shot=async(page,name)=>{
  const file=`visual/${name}.png`,bytes=await page.screenshot({path:'/evidence/'+file,fullPage:true})
  evidence.screenshots.push({file,sha256:createHash('sha256').update(bytes).digest('hex'),viewport:page.viewportSize(),url:page.url()})
 }
 const newPage=async(width,dark)=>{
  const context=await browser.newContext({viewport:{width,height:900},deviceScaleFactor:1,colorScheme:dark?'dark':'light',locale:'en-US'})
  contexts.push(context)
  // Preferences only: auth tokens/users are set by the production login flow.
  await context.addInitScript(({dark})=>{localStorage.setItem('theme',dark?'dark':'light');localStorage.setItem('sub2api_locale','en');document.addEventListener('DOMContentLoaded',()=>document.documentElement.classList.toggle('dark',dark),{once:true})},{dark})
  const page=await context.newPage();page.setDefaultTimeout(15000)
  page.on('pageerror',error=>evidence.errors.push({url:page.url(),message:error.message}))
  page.on('request',request=>evidence.network.push({url:request.url(),method:request.method()}))
  return page
 }
 const settle=async(page)=>{await page.evaluate(async()=>{await document.fonts.ready;await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))});await page.waitForTimeout(300)}
 const layout=async(page,label)=>{
  const value=await page.evaluate(()=>({width:innerWidth,scrollWidth:document.documentElement.scrollWidth,height:document.body.scrollHeight,text:document.body.innerText.slice(0,800),dark:document.documentElement.classList.contains('dark')}))
  assert.ok(value.text.trim().length>20,label+': actual content required')
  assert.ok(value.scrollWidth<=value.width+2,label+': document horizontal overflow')
  assert.ok(!value.text.includes('NaN') && !value.text.includes('[object Object]'),label+': invalid displayed values')
  return value
 }
 const canvasSnapshot=page=>page.evaluate(()=>{
  const c=window.visual.chart();if(!c)throw new Error('real Chart.js instance missing')
  const canvas=c.canvas,ctx=canvas.getContext('2d'),bytes=ctx.getImageData(0,0,canvas.width,canvas.height).data
  let hash=2166136261,ink=0;for(let i=0;i<bytes.length;i++){hash=Math.imul(hash^bytes[i],16777619)>>>0;if(i%4===3&&bytes[i])ink++}
  const near=p=>{const dpr=c.currentDevicePixelRatio,x=Math.max(0,Math.min(canvas.width-9,Math.round(p.x*dpr)-4)),y=Math.max(0,Math.min(canvas.height-9,Math.round(p.y*dpr)-4));return [...ctx.getImageData(x,y,9,9).data].filter((v,i)=>i%4===3&&v>0).length}
  const ds=c.data.datasets,sc=c.options.scales
  return {id:c.id,sameCanvas:!window.__visualCanvas||window.__visualCanvas===canvas,width:canvas.width,height:canvas.height,ink,pixelHash:hash,chartArea:c.chartArea,
   datasets:ds.map((d,i)=>({label:d.label,data:[...d.data],dash:d.borderDash||[],point:c.getDatasetMeta(i).controller.getStyle(0).pointStyle,points:c.getDatasetMeta(i).data.map(p=>({x:p.x,y:p.y,ink:near(p)}))})),
   legend:c.legend.legendItems.map((item,i)=>({text:item.text,point:item.pointStyle,box:c.legend.legendHitBoxes[i]})),
   colors:{line:ds[0].borderColor,fill:ds[0].backgroundColor,point:ds[0].pointBackgroundColor,muted:ds[1].borderColor,faint:ds[2].borderColor,hitRate:ds[4].borderColor,legend:c.options.plugins.legend.labels.color,xGrid:sc.x.grid.color,yGrid:sc.y.grid.color,xText:sc.x.ticks.color,yText:sc.y.ticks.color,percentText:sc.yPercent.ticks.color}}
 })
 const painted=async(page)=>{
  await page.waitForFunction(()=>window.visual?.chart()?.getDatasetMeta(0).data.length>0)
  let previous='',stable=0,last
  for(let i=0;i<35;i++){await page.waitForTimeout(100);last=await canvasSnapshot(page);const key=JSON.stringify(last);stable=key===previous?stable+1:0;previous=key;if(stable>=3)return last}
  throw new Error('Chart drawing did not settle')
 }
 const tableSnapshot=page=>page.evaluate(()=>{
  const wrapper=document.querySelector('#table-frame .table-wrapper'),rect=wrapper.getBoundingClientRect(),head=wrapper.querySelector('thead').getBoundingClientRect()
  const rows=[...wrapper.querySelectorAll('tbody tr[data-row-id]')].map(el=>{const r=el.getBoundingClientRect();return {id:el.dataset.rowId,index:Number(el.dataset.index),top:r.top,bottom:r.bottom,height:r.height}})
  const bottom=rect.top+wrapper.clientHeight,contentTop=Math.max(rect.top,head.bottom),visible=rows.filter(r=>r.bottom>contentTop+1&&r.top<bottom-1)
  return {...window.visual.table(),rows,visible,contentTop,bottom,scrollTop:wrapper.scrollTop,scrollHeight:wrapper.scrollHeight,clientHeight:wrapper.clientHeight,lastDataBottom:rows.at(-1)?.bottom}
 })
 const tableAt=async(page,fraction,virtual,label)=>{
  const scroll=page.locator('#table-frame .table-wrapper')
  // Measured variable rows may grow total size. Re-anchor the bottom until the
  // actual last key is visible rather than trusting the initial estimate.
  for(let i=0;i<8;i++){
   await scroll.evaluate((el,f)=>{el.scrollTop=(el.scrollHeight-el.clientHeight)*f},fraction);await settle(page)
   const s=await tableSnapshot(page)
   if(fraction!==1||s.rows.at(-1)?.id===String(s.ids.at(-1)))break
  }
  const samples=[];for(let i=0;i<3;i++){await page.waitForTimeout(150);samples.push(await scroll.evaluate(el=>el.scrollTop))}
  assertStableScroll(samples)
  const snap=await tableSnapshot(page);assertTableSnapshot(snap,{virtual,atEnd:fraction===1})
  evidence.tables.push({label,fraction,viewport:page.viewportSize(),samples,...snap});return snap
 }
 const contractReadback=async(page)=>{
  const marker=page.locator('[data-testid="app-deployment-contract"]');await marker.waitFor({state:'attached'})
  assert.deepEqual(JSON.parse(await marker.getAttribute('data-contract')),contract)
  assert.match(await page.locator('script[type=module][src]').first().getAttribute('src'),/^\/assets\/index-[\w-]+\.js$/)
 }
 const login=async(page,role,captureOnboarding)=>{
  await page.goto(panel+'/login');await contractReadback(page)
  await page.locator('#email').fill(`${role}@fixture.invalid`);await page.locator('#password').fill('fixture-password')
  const response=page.waitForResponse(r=>r.url()===panel+'/api/v1/auth/login'&&r.request().method()==='POST')
  await page.locator('button[type=submit]').click();assert.equal((await response).status(),200)
  await page.waitForURL(url=>url.pathname!=='/login');await settle(page)
  if(role==='admin'){
   const popover=page.locator('.driver-popover');await popover.waitFor({state:'visible',timeout:15000})
   if(captureOnboarding){
    const rect=await popover.boundingBox();assert.ok(rect&&rect.x>=-1&&rect.x+rect.width<=page.viewportSize().width+2&&rect.y>=0)
    evidence.pages.push({name:'onboarding',viewport:page.viewportSize(),dark:await page.evaluate(()=>document.documentElement.classList.contains('dark')),rect,actualApp:true})
    await shot(page,captureOnboarding)
    const next=popover.locator('.driver-popover-next-btn');assert.ok(await next.isVisible());await next.focus();assert.equal(await next.evaluate(el=>el===document.activeElement),true)
   }
   await page.keyboard.press('Escape');await popover.waitFor({state:'hidden'})
  }
 }
 try {
  await control({visual:'normal',status:200,setupHang:'',faultMode:'none',setup:{code:0,data:{needs_setup:false,step:'completed'}}})
  for(const width of viewports)for(const initialDark of [false,true]){
   const page=await newPage(width,initialDark)
   await page.goto(panel+'/__d0__/visual.html');await page.waitForFunction(()=>window.visual)
   assert.deepEqual(await page.evaluate(()=>window.visual.contract),contract)
   const css=await page.evaluate(()=>window.visualCSS);assert.ok(css.length>0)
   for(const kind of ['crossing','overlap','zero']){
    await page.evaluate(kind=>window.visual.configure({kind}),kind)
    let before=await painted(page);assertChartSnapshot(before)
    assert.equal(await page.evaluate(()=>document.documentElement.classList.contains('dark')),initialDark)
    await page.evaluate(()=>{window.__visualCanvas=window.visual.chart().canvas})
    const states=[{theme:initialDark?'dark':'light',snapshot:before}]
    await shot(page,`chart-${width}-${initialDark}-${kind}-initial`)
    for(let n=0;n<2;n++){
     await page.locator('#theme-toggle').click();const after=await painted(page);assertThemeTransition(before,after)
     states.push({theme:await page.evaluate(()=>document.documentElement.classList.contains('dark')?'dark':'light'),snapshot:after})
     await shot(page,`chart-${width}-${initialDark}-${kind}-toggle-${n}`);before=after
    }
    assert.deepEqual(states.at(-1).snapshot.colors,states[0].snapshot.colors,'round-trip restores all initial colors')
    // Trigger a genuine pointer event over the plotted point, not a fabricated tooltip.
    const box=await page.locator('#chart-fixture canvas').boundingBox(),point=before.datasets[4].points[1]
    await page.mouse.move(box.x+point.x,box.y+point.y)
    await page.waitForFunction(()=>window.visual.chart().tooltip?.opacity>0 && window.visual.chart().tooltip?.dataPoints?.some(p=>p.dataset.label==='Cache Hit Rate'))
    const tooltip=await page.evaluate(()=>{const t=window.visual.chart().tooltip;return {opacity:t.opacity,body:t.body.map(b=>b.lines.join(' ')),footer:t.footer}})
    assert.ok(tooltip.opacity>0);assert.ok(tooltip.body.some(x=>x.includes(before.datasets[4].data[1].toFixed(1)+'%')))
    assert.ok(tooltip.footer.join(' ').includes('Actual: $0.010') && tooltip.footer.join(' ').includes('Standard: $0.020'))
    evidence.charts.push({width,initialDark,kind,css,states,tooltip,actualCanvas:true})
    await page.mouse.move(0,0)
   }
   for(const state of ['loading','empty']){
    await page.evaluate(state=>window.visual.configure({state}),state);await settle(page)
    assert.equal(await page.locator('#chart-fixture canvas').count(),0)
    if(state==='loading')assert.ok(await page.locator('#table-frame .animate-pulse').count()>0)
    else assert.match(await page.locator('#table-frame').innerText(),/no data/i)
    await shot(page,`components-${width}-${initialDark}-${state}`)
   }
   await page.context().close()
  }
  for(const dark of [false,true]){
   const page=await newPage(1440,dark);await page.goto(panel+'/__d0__/visual.html');await page.waitForFunction(()=>window.visual)
   for(const count of [5,100,101,500]){
    await page.evaluate(count=>window.visual.configure({count,state:'normal',paginated:false}),count);await settle(page)
    for(const fraction of [0,.5,1,0])await tableAt(page,fraction,count>100,`${dark}-${count}`)
    const row=page.locator('#table-frame tr[data-row-id="1"]');await row.locator('input[type=checkbox]').check()
    assert.ok((await page.evaluate(()=>window.visual.table())).selected.includes(1))
    await row.locator('[data-action="1"]').click();await row.getByRole('menu').waitFor();await row.getByRole('menuitem').click()
    assert.equal((await page.evaluate(()=>window.visual.table())).lastAction,1)
    assert.equal(await row.getByRole('menu').count(),0)
    const sortHeader=page.locator('#table-frame th[aria-sort]').first()
    for(let n=0;n<3 && await sortHeader.getAttribute('aria-sort')!=='descending';n++){await sortHeader.click();await settle(page)}
    assert.equal((await page.evaluate(()=>window.visual.table())).ids[0],count)
    await tableAt(page,0,count>100,`${dark}-${count}-sorted`)
    await shot(page,`table-${dark}-${count}-sorted`)
    // Return ascending, then filter through the real parent input.
    await page.locator('#table-frame th[aria-sort]').first().click();await page.locator('#row-filter').fill('Row 1 —');await settle(page)
    assert.deepEqual((await page.evaluate(()=>window.visual.table())).ids,[1]);await tableAt(page,0,false,`${dark}-${count}-filtered`)
   }
   await page.evaluate(()=>window.visual.configure({count:500,paginated:true}));await settle(page)
   await page.getByRole('button',{name:'Go to page 2',exact:true}).click();await settle(page)
   assert.equal((await page.evaluate(()=>window.visual.table())).page,2)
   const second=await tableAt(page,1,true,`${dark}-page-2`);assert.equal(second.ids[0],102);assert.equal(second.ids.at(-1),202)
   await page.getByRole('button',{name:'Go to page 1',exact:true}).click();await settle(page)
   await tableAt(page,0,true,`${dark}-page-1-return`)
   await page.setViewportSize({width:768,height:700});await settle(page);await tableAt(page,.5,true,`${dark}-resize-768`)
   await page.setViewportSize({width:320,height:900});await settle(page)
   assert.equal(await page.locator('#table-frame .table-wrapper').count(),0,'mobile uses actual card branch')
   assert.equal(await page.locator('#table-frame [data-fixture-id]').count(),101)
   await page.setViewportSize({width:1440,height:900});await settle(page);await tableAt(page,1,true,`${dark}-resize-return`)
   await shot(page,`table-${dark}-resize-return`);await page.context().close()
  }
  // Production routes are never replaced with component mocks or forged stores.
  for(const width of viewports)for(const dark of [false,true]){
   for(const role of [null,'user','admin']){
    const page=await newPage(width,dark)
    if(role)await login(page,role,role==='admin'?`app-onboarding-${width}-${dark}`:null)
    for(const spec of pageCases.filter(x=>x.role===role)){
     const pending=spec.api?page.waitForResponse(r=>new URL(r.url()).pathname===spec.api&&r.status()===200):null
     await page.goto(panel+spec.path);await contractReadback(page);if(pending)await pending
     await page.locator(spec.ready).first().waitFor();await settle(page)
     assert.equal(new URL(page.url()).pathname,spec.path,'auth/router must keep intended page')
     const measured=await layout(page,spec.name)
     assert.equal(measured.dark,dark)
     const input=page.locator('input:not([type=hidden]):visible').first()
     let focus=null;if(await input.count()){
      await input.blur();await settle(page)
      const before=await input.evaluate(el=>{const s=getComputedStyle(el);return {border:s.borderColor,outline:s.outline,shadow:s.boxShadow}})
      await input.focus();await settle(page)
      focus=await input.evaluate(el=>{const s=getComputedStyle(el);return {active:el===document.activeElement,border:s.borderColor,outline:s.outline,shadow:s.boxShadow}})
      assert.equal(focus.active,true);focus.before=before
      assert.ok(['border','outline','shadow'].some(k=>before[k]!==focus[k]),spec.name+': visible focus style required')
     }
     const button=page.locator('button:visible:not([disabled])').first()
     let hover=null;if(await button.count()){
      await page.mouse.move(0,0)
      const before=await button.evaluate(el=>{const s=getComputedStyle(el);return {background:s.backgroundColor,color:s.color,shadow:s.boxShadow}})
      await button.hover();await settle(page)
      hover=await button.evaluate(el=>{const s=getComputedStyle(el);return {hovered:el.matches(':hover'),background:s.backgroundColor,color:s.color,shadow:s.boxShadow}})
      assert.equal(hover.hovered,true);hover.before=before
     }
     evidence.pages.push({name:spec.name,path:spec.path,width,dark,actualApp:true,measured,focus,hover,interactionCoverage:{focus:focus?'checked':'no visible input on this route',hover:hover?'checked':'no visible enabled button on this route'},api:spec.api||null})
     await shot(page,`app-${spec.name}-${width}-${dark}`)
     if(spec.name==='key-usage'){
      const key=page.locator('input[type=password]');await key.fill('API-KEY-SENTINEL')
      const result=page.waitForResponse(r=>new URL(r.url()).origin===gateway&&new URL(r.url()).pathname==='/v1/usage')
      await key.locator('xpath=../..').locator('button').last().click();assert.equal((await result).status(),200);await settle(page)
      await layout(page,'KeyUsage result');await shot(page,`app-key-usage-result-${width}-${dark}`)
     }
    }
    await page.context().close()
   }
  }
  // State changes are explicit upstream fixtures, including actual loading and
  // disabled form controls. Unknown API requests cannot masquerade as empty UI.
  for(const mode of ['loading','error','empty']){
   await control({visual:mode})
   const page=await newPage(768,false)
   if(mode==='loading'){
    await page.goto(panel+'/login');await page.locator('#email').fill('user@fixture.invalid');await page.locator('#password').fill('fixture-password')
    const response=page.waitForResponse(r=>r.url()===panel+'/api/v1/auth/login')
    await page.locator('button[type=submit]').click()
    await page.waitForFunction(()=>document.querySelector('#email')?.disabled)
    await shot(page,'app-login-loading-disabled');assert.equal((await response).status(),200)
    evidence.pages.push({name:'login',state:mode,disabled:true,actualApp:true})
   } else if(mode==='error'){
    await page.goto(panel+'/login');await page.locator('#email').fill('user@fixture.invalid');await page.locator('#password').fill('invalid-password')
    await page.locator('button[type=submit]').click();await page.getByText('Fixture login rejected',{exact:false}).first().waitFor()
    await shot(page,'app-login-error');evidence.pages.push({name:'login',state:mode,actualApp:true})
   } else {
    await login(page,'user',null);await page.goto(panel+'/dashboard');await page.locator('main .card').first().waitFor();await settle(page)
    assert.match(await page.locator('main').innerText(),/no data|no usage/i)
    await shot(page,'app-user-dashboard-empty');evidence.pages.push({name:'user-dashboard',state:mode,actualApp:true})
   }
   await page.context().close()
  }
  evidence.observations=(await observed()).filter(row=>row.visual)
  assert.ok(evidence.observations.some(row=>row.url==='/api/v1/auth/login'&&row.status===200))
  assert.ok(!evidence.observations.some(row=>row.unmappedVisualAPI),'every requested visual API must have an explicit fixture')
  for(const request of evidence.network){const u=new URL(request.url);assert.ok([panel,gateway].includes(u.origin),'visual request outside panel/gateway boundary: '+u.origin)}
  assert.deepEqual(evidence.errors,[],'no uncaught page/render errors')
  evidence.outcome='passed'
 } catch(error){
  evidence.error={message:error.message,stack:error.stack}
  for(const context of contexts){for(const page of context.pages())if(!page.isClosed())await shot(page,`failure-${evidence.screenshots.length}`).catch(e=>{evidence.screenshotError=String(e)})}
  throw error
 }
 finally {
  // Restore the D0 mock before the existing fault injection/recovery stages.
  try{await control({visual:'off',status:200,setupHang:''})}
  catch(error){cleanupError=error;evidence.restoreError=String(error);evidence.outcome='failed'}
  finally{
   for(const context of contexts)await context.close().catch(()=>{})
   try{await writeFile('/evidence/visual.json',JSON.stringify(evidence,null,2))}catch(error){cleanupError??=error}
  }
 }
 if(cleanupError)throw cleanupError
 return {outcome:evidence.outcome,charts:evidence.charts.length,tables:evidence.tables.length,pages:evidence.pages.length,screenshots:evidence.screenshots.length,candidate:contract.candidate}
}
