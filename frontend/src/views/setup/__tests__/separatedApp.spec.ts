import { it, expect, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
vi.mock('@/views/auth/LoginView.vue',()=>({default:{template:'<div data-testid="login">Login</div>'}}))
const wizardMounted=vi.hoisted(()=>vi.fn())
vi.mock('@/views/setup/SetupWizardView.vue',()=>({default:{setup(){wizardMounted()},template:'<form data-testid="wizard" />'}}))
afterEach(()=>{vi.useRealTimers();vi.unstubAllEnvs();vi.unstubAllGlobals();vi.restoreAllMocks()})
it('real App and router never mount wizard during loading/error/retry/history navigation',async()=>{
  vi.resetModules();localStorage.clear();vi.stubGlobal('scrollTo',vi.fn())
  vi.stubEnv('VITE_DEPLOYMENT_MODE','separated');vi.stubEnv('VITE_API_BASE_URL','/api/v1')
  vi.stubEnv('VITE_PANEL_PUBLIC_ORIGIN','http://localhost:3000');vi.stubEnv('VITE_GATEWAY_BASE_URL','http://localhost:8089')
  const pinia=createPinia();setActivePinia(pinia)
  const appStore=(await import('@/stores/app')).useAppStore()
  vi.spyOn(appStore,'fetchPublicSettings').mockResolvedValue(undefined)
  let complete:(v:any)=>void=()=>{}
  const f=vi.fn().mockImplementation(()=>new Promise(r=>{complete=r}));vi.stubGlobal('fetch',f)
  const router=(await import('@/router')).default
  await router.push('/setup');await router.isReady()
  const wrapper=mount((await import('@/App.vue')).default,{global:{plugins:[pinia,router,createI18n({legacy:false,locale:'en',messages:{en:{home:{login:"Login"}}}})],stubs:{Toast:true,NavigationProgress:true,AnnouncementPopup:true,AdminComplianceDialog:true}}})
  expect(wrapper.find('[data-testid="setup-unavailable"]').exists()).toBe(true);expect(wizardMounted).not.toHaveBeenCalled()
  complete({ok:false,headers:new Headers()});await flushPromises()
  expect(wrapper.text()).toContain('unavailable')
  f.mockResolvedValueOnce({ok:true,headers:new Headers({'Content-Type':'application/json'}),json:async()=>({code:0,data:{needs_setup:true,step:'welcome'}})})
  await wrapper.find('button').trigger('click');await flushPromises();expect(wrapper.text()).toContain('incomplete')
  await router.push('/setup');await flushPromises();expect(wizardMounted).not.toHaveBeenCalled()
  f.mockResolvedValueOnce({ok:true,headers:new Headers({'Content-Type':'application/json'}),json:async()=>({code:0,data:{needs_setup:false,step:'completed'}})})
  await wrapper.find('button').trigger('click');await flushPromises();expect(wrapper.find('[data-testid="setup-unavailable"]').exists()).toBe(false)
  await router.push('/setup');await flushPromises();expect(router.currentRoute.value.path).toBe('/login');expect(wizardMounted).not.toHaveBeenCalled()
  router.back();await flushPromises();expect(wizardMounted).not.toHaveBeenCalled()
  wrapper.unmount();router.options.history.destroy()
})
it('monolith retains the wizard route and mount behavior',async()=>{
 vi.resetModules();localStorage.clear();vi.stubGlobal('scrollTo',vi.fn());vi.stubEnv('VITE_DEPLOYMENT_MODE','')
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue({ok:true,headers:new Headers({'Content-Type':'application/json'}),json:async()=>({code:0,data:{needs_setup:true,step:'welcome'}})}))
 const pinia=createPinia();setActivePinia(pinia)
 vi.spyOn((await import('@/stores/app')).useAppStore(),'fetchPublicSettings').mockResolvedValue(undefined)
 const router=(await import('@/router')).default
 await router.push('/setup');await router.isReady()
 const wrapper=mount((await import('@/App.vue')).default,{global:{plugins:[pinia,router,createI18n({legacy:false,locale:'en',messages:{en:{}}})],stubs:{Toast:true,NavigationProgress:true,AnnouncementPopup:true,AdminComplianceDialog:true}}})
 await flushPromises();expect(wizardMounted).toHaveBeenCalled();expect(wrapper.find('[data-testid="wizard"]').exists()).toBe(true)
 wrapper.unmount();router.options.history.destroy()
})


it.each(['headers', 'body'])('real App recovers from stalled %s at 30s and retries to ready', async (phase) => {
  vi.resetModules(); localStorage.clear(); wizardMounted.mockClear()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubEnv('VITE_DEPLOYMENT_MODE', 'separated'); vi.stubEnv('VITE_API_BASE_URL', '/api/v1')
  vi.stubEnv('VITE_PANEL_PUBLIC_ORIGIN', 'http://localhost:3000'); vi.stubEnv('VITE_GATEWAY_BASE_URL', 'http://localhost:8089')
  const pinia = createPinia(); setActivePinia(pinia)
  const settings = vi.spyOn((await import('@/stores/app')).useAppStore(), 'fetchPublicSettings').mockResolvedValue(undefined)
  const ready = { ok: true, headers: new Headers({ 'Content-Type': 'application/json' }), json: async () => ({ code: 0, data: { needs_setup: false, step: 'completed' } }) }
  let finishLate!: (value: unknown) => void
  const pending = new Promise(resolve => { finishLate = resolve })
  const fetcher = vi.fn().mockImplementationOnce(() => phase === 'headers' ? pending : Promise.resolve({ ...ready, json: () => pending })).mockResolvedValueOnce(ready)
  vi.stubGlobal('fetch', fetcher)
  const router = (await import('@/router')).default
  await router.push('/setup'); await router.isReady()
  const wrapper = mount((await import('@/App.vue')).default, { global: { plugins: [pinia, router, createI18n({ legacy: false, locale: 'en', messages: { en: { home: { login: 'Login' } } } })], stubs: { Toast: true, NavigationProgress: true, AnnouncementPopup: true, AdminComplianceDialog: true } } })
  try {
    await flushPromises()
    expect(wrapper.text()).toContain('Checking service status')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('[data-testid="login"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(29999); await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(wrapper.text()).toContain('Service status is unavailable')
    expect(wrapper.find('button').text()).toBe('Retry')
    const firstSignal = fetcher.mock.calls[0][1].signal
    expect(firstSignal.aborted).toBe(true)
    await wrapper.find('button').trigger('click'); await flushPromises()
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(fetcher.mock.calls[1][1].signal).not.toBe(firstSignal)
    expect(wrapper.find('[data-testid="setup-unavailable"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="login"]').exists()).toBe(true)
    expect(settings).toHaveBeenCalledOnce()
    // A late old response cannot overwrite the successfully retried App state.
    finishLate(phase === 'headers' ? ready : { code: 0, data: { needs_setup: true, step: 'welcome' } })
    await flushPromises()
    expect(wrapper.find('[data-testid="login"]').exists()).toBe(true)
    expect(wizardMounted).not.toHaveBeenCalled()
  } finally { wrapper.unmount(); router.options.history.destroy() }
})

it('monolith setup router guard falls back after the original 30s timeout', async () => {
  vi.resetModules(); localStorage.clear()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.stubGlobal('scrollTo', vi.fn()); vi.stubEnv('VITE_DEPLOYMENT_MODE', '')
  vi.stubEnv('VITE_API_BASE_URL', '/api/v1')
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
  const pinia = createPinia(); setActivePinia(pinia)
  const router = (await import('@/router')).default
  let navigated = false
  const navigation = router.push('/setup').then(() => { navigated = true })
  try {
    await flushPromises()
    await vi.advanceTimersByTimeAsync(29999)
    expect(navigated).toBe(false)
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(navigated).toBe(true)
    await navigation
    expect(router.currentRoute.value.path).toBe('/setup')
  } finally { router.options.history.destroy() }
})


it.each(['separated', ''])('real App cancels pending status on unmount in mode %j', async (mode) => {
  vi.resetModules(); localStorage.clear()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubEnv('VITE_DEPLOYMENT_MODE', mode); vi.stubEnv('VITE_API_BASE_URL', '/api/v1')
  vi.stubEnv('VITE_PANEL_PUBLIC_ORIGIN', 'http://localhost:3000'); vi.stubEnv('VITE_GATEWAY_BASE_URL', 'http://localhost:8089')
  const pinia = createPinia(); setActivePinia(pinia)
  const settings = vi.spyOn((await import('@/stores/app')).useAppStore(), 'fetchPublicSettings').mockResolvedValue(undefined)
  const fetcher = vi.fn(() => new Promise(() => {})); vi.stubGlobal('fetch', fetcher)
  const router = (await import('@/router')).default
  await router.push('/login'); await router.isReady()
  const wrapper = mount((await import('@/App.vue')).default, { global: { plugins: [pinia, router, createI18n({ legacy: false, locale: 'en', messages: { en: { home: { login: 'Login' } } } })], stubs: { Toast: true, NavigationProgress: true, AnnouncementPopup: true, AdminComplianceDialog: true } } })
  await flushPromises()
  expect(fetcher).toHaveBeenCalledOnce()
  expect(fetcher.mock.calls[0][1].signal.aborted).toBe(false)
  wrapper.unmount(); router.options.history.destroy()
  await flushPromises()
  expect(fetcher.mock.calls[0][1].signal.aborted).toBe(true)
  await vi.advanceTimersByTimeAsync(30000); await flushPromises()
  expect(settings).not.toHaveBeenCalled()
})

it('monolith App continues normal initialization after the status deadline', async () => {
  vi.resetModules(); localStorage.clear()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.stubGlobal('scrollTo', vi.fn()); vi.stubEnv('VITE_DEPLOYMENT_MODE', '')
  vi.stubEnv('VITE_API_BASE_URL', '/api/v1')
  const pinia = createPinia(); setActivePinia(pinia)
  const settings = vi.spyOn((await import('@/stores/app')).useAppStore(), 'fetchPublicSettings').mockResolvedValue(undefined)
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
  const router = (await import('@/router')).default
  await router.push('/login'); await router.isReady()
  const wrapper = mount((await import('@/App.vue')).default, { global: { plugins: [pinia, router, createI18n({ legacy: false, locale: 'en', messages: { en: { home: { login: 'Login' } } } })], stubs: { Toast: true, NavigationProgress: true, AnnouncementPopup: true, AdminComplianceDialog: true } } })
  try {
    await vi.advanceTimersByTimeAsync(29999); await flushPromises()
    expect(settings).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(settings).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="login"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="setup-unavailable"]').exists()).toBe(false)
  } finally { wrapper.unmount(); router.options.history.destroy() }
})
