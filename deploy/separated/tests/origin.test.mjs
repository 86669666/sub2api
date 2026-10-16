import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { normalizeOrigin, originPair, safePath, gatewayPath } from '../origin.mjs'
import { renderConfig } from '../render-config.mjs'
export const validOrigins = [
  [' https://API.Example:443/ ', 'https://api.example'],
  ['https://例子.测试', 'https://xn--fsqu00a.xn--0zwm56d'],
  ['http://[::1]:8089/', 'http://[::1]:8089'],
  ['http://localhost:8089', 'http://localhost:8089'],
]
export const invalidOrigins = ['https://a/x/..','https://a/%2e','https://a?','https://a#','https://$host',
  'https://a;xx','https://a"','https://*.a','https://u@a','//a','/a','https://a//','https://a\\x',
  'https://a\n','https://a.','http://example.com','http://127.1','http://2130706433','http://0177.0.0.1',
  'https://a:{x}','https://a b','https://a:0/path','https://a/%252e', 'https://a/#']
test('raw origin rejection and canonical DNS/IP grammar', () => {
  for (const [input, output] of validOrigins) assert.equal(normalizeOrigin(input),output)
  for (const input of invalidOrigins) assert.throws(()=>normalizeOrigin(input), input)
  assert.throws(()=>originPair('https://a','https://a/'))
  assert.throws(()=>originPair('https://a','http://localhost:8089'))
})
test('path and query have different decoding boundaries', () => {
  for(const input of ['/v1/images/batches/a%2Fb','/v1/images/batches/%2e','/v1/images/batches/%252e','/v1//usage','//evil','https://evil','/v1/./usage','/v1/images/batchesX','/api/v1/admin']) assert.throws(()=>gatewayPath(input),input)
  assert.equal(gatewayPath('v1/images/batches?q=https%3A%2F%2Fa%2Fx'),'/v1/images/batches?q=https%3A%2F%2Fa%2Fx')
  assert.equal(safePath('/setup/status'),'/setup/status')
})
test('renderer binds exact origins and candidate to all artifacts', async () => {
  const dir=await mkdtemp(join(tmpdir(),'sub2api-render-'))
  try {
    const m=await renderConfig('http://127.0.0.1:8088','http://127.0.0.1:8089','a'.repeat(40),dir)
    assert.deepEqual(JSON.parse(await readFile(join(dir,'deployment-manifest.json'),'utf8')),m)
    const cors=await readFile(join(dir,'gateway.nginx.conf'),'utf8')
    assert.ok(cors.includes('~^http://127\\\\.0\\\\.0\\\\.1:8088$'))
    const csp=await readFile(join(dir,'security_headers.conf'),'utf8')
    assert.ok(csp.includes("connect-src 'self' http://127.0.0.1:8089 ws://127.0.0.1:8088;"))
    assert.ok(!csp.includes(' ws: ')); assert.ok(!cors.includes('@@'))
    assert.match(await readFile(join(dir,'frontend.env'),'utf8'), /VITE_BUILD_COMMIT=a{40}\n/)
    await assert.rejects(renderConfig('https://a','https://b','unknown',dir))
  } finally {await rm(dir,{recursive:true,force:true})}
})
