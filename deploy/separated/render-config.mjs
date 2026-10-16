import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve, dirname } from 'node:path'
import { originPair } from './origin.mjs'

export async function renderConfig(panel, gateway, candidate, output) {
  const pair = originPair(panel, gateway)
  if (!/^[a-f0-9]{40}$/.test(candidate)) throw new Error('An exact source commit is required')
  const manifest = { format: 1, mode: 'separated', candidate, ...pair }
  const here = dirname(fileURLToPath(import.meta.url))
  // Escape the regex, then the Nginx quoted-string layer.
  const originRegex = [...pair.panel].map(c => '.+?^$()[]{}|*'.includes(c) ? String.fromCharCode(92, 92) + c : c).join('')
  const replacements = {
    '@@PANEL@@': pair.panel, '@@GATEWAY@@': pair.gateway,
    '@@PANEL_WS@@': pair.panel.replace(/^http/, 'ws'), '@@ORIGIN_REGEX@@': originRegex,
  }
  await mkdir(output, { recursive: true })
  for (const name of ['panel.nginx.conf', 'gateway.nginx.conf', 'security_headers.conf']) {
    let text = await readFile(resolve(here, name), 'utf8')
    for (const [key, value] of Object.entries(replacements)) text = text.replaceAll(key, value)
    if (text.includes('@@')) throw new Error('Unresolved configuration marker')
    await writeFile(resolve(output, name), text)
  }
  await writeFile(resolve(output, 'deployment-manifest.json'), JSON.stringify(manifest) + '\n')
  // Vite .env.production contains only public, validated literals.
  await writeFile(resolve(output, 'frontend.env'), `VITE_DEPLOYMENT_MODE=separated\nVITE_API_BASE_URL=/api/v1\nVITE_GATEWAY_BASE_URL=${pair.gateway}\nVITE_PANEL_PUBLIC_ORIGIN=${pair.panel}\nVITE_BUILD_COMMIT=${candidate}\n`)
  return manifest
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await renderConfig(...process.argv.slice(2))
}
