import {it,expect,vi,afterEach} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {createI18n} from 'vue-i18n'
afterEach(()=>{vi.unstubAllEnvs();vi.unstubAllGlobals();vi.restoreAllMocks()})
it('real KeyUsage action uses API key only on the gateway',async()=>{
 vi.stubEnv('VITE_API_BASE_URL','/api/v1')
 vi.stubEnv('VITE_DEPLOYMENT_MODE','separated');vi.stubEnv('VITE_GATEWAY_BASE_URL','http://localhost:8089');vi.stubEnv('VITE_PANEL_PUBLIC_ORIGIN','http://localhost:3000')
 const pinia=createPinia();setActivePinia(pinia)
 vi.spyOn((await import('@/stores/app')).useAppStore(),'fetchPublicSettings').mockResolvedValue(undefined)
 const f=vi.fn().mockResolvedValue({ok:false,status:401,json:async()=>({message:'fixture'})});vi.stubGlobal('fetch',f)
 localStorage.setItem('auth_token','JWT-SENTINEL')
 const wrapper=mount((await import('@/views/KeyUsageView.vue')).default,{global:{plugins:[pinia,createI18n({legacy:false,locale:'en',missingWarn:false,fallbackWarn:false,messages:{en:{}}})],stubs:{RouterLink:true,LocaleSwitcher:true,Icon:true}}})
 await wrapper.find('input[type=password]').setValue('API-KEY-SENTINEL')
 await wrapper.findAll('button').find(b=>b.text()==='keyUsage.query')!.trigger('click');await flushPromises()
 expect(f).toHaveBeenCalledOnce();const [url,init]=f.mock.calls[0]
 expect(url).toMatch(/^http:\/\/localhost:8089\/v1\/usage/);expect(init).toMatchObject({credentials:'omit',redirect:'error',headers:{Authorization:'Bearer API-KEY-SENTINEL'}})
 expect(JSON.stringify(init)).not.toContain('JWT-SENTINEL');wrapper.unmount()
})
