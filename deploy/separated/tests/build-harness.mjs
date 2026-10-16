import { createRequire } from 'node:module'
import { resolve, dirname } from 'node:path'
import { pathToFileURL } from 'node:url'
const frontend = resolve(import.meta.dirname, '../../../frontend')
const require = createRequire(resolve(frontend, 'package.json'))
const {build, loadEnv} = await import(pathToFileURL(resolve(dirname(require.resolve('vite/package.json')), 'dist/node/index.js')).href)
const { default: vue } = await import(require.resolve('@vitejs/plugin-vue'))
const env = loadEnv('production', frontend, 'VITE_')
// Same public build inputs as the production app; no process-env spreading.
const keys=['VITE_DEPLOYMENT_MODE','VITE_API_BASE_URL','VITE_GATEWAY_BASE_URL','VITE_PANEL_PUBLIC_ORIGIN','VITE_BUILD_COMMIT']
await build({configFile:false,plugins:[vue()],root:import.meta.dirname,base:'/__d0__/',resolve:{alias:{'@':resolve(frontend,'src'),'vue':require.resolve('vue/dist/vue.runtime.esm-bundler.js')}},define:Object.fromEntries(keys.map(k=>['import.meta.env.'+k,JSON.stringify(env[k])])),build:{outDir:process.argv[2] || '/out/harness',emptyOutDir:true,rollupOptions:{input:resolve(import.meta.dirname,'harness.html')}}})
