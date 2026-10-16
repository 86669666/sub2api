import { it, expect, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
vi.mock('@/views/auth/LoginView.vue',()=>({default:{template:'<div data-testid="login">Login</div>'}}))
const wizardMounted=vi.hoisted(()=>vi.fn())
vi.mock('@/views/setup/SetupWizardView.vue',()=>({default:{setup(){wizardMounted()},template:'<form data-testid="wizard" />'}}))
afterEach(()=>{vi.unstubAllEnvs();vi.unstubAllGlobals();vi.restoreAllMocks()})
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
