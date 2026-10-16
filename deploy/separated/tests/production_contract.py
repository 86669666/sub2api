"""Evaluate the exact URL module imported by the formal App, without a browser.

The supported Rollup layout emits url.ts once as a content-hashed deployment-contract chunk with
stable exports. The real App imports/calls that module. We execute only its pure
getDeploymentContract in a bounded Node VM; all imports and dynamic imports in
that module are forbidden. Full App consumption is additionally checked by the
real-browser probe, not inferred from a bag of strings or a harness result.
"""
import hashlib
import json
from html.parser import HTMLParser
import os
import re
import shutil
import subprocess


NODE_PROBE = r"""
import vm from 'node:vm';
let input = '';
for await (const part of process.stdin) input += part;
const {entry, code, panel, readerSpecifier} = JSON.parse(input);
const context = vm.createContext({URL, window: Object.freeze({location: Object.freeze({origin: panel})})});
const entryModule = new vm.SourceTextModule(entry, {context});
if (!entryModule.dependencySpecifiers.includes(readerSpecifier))
  throw new Error('App entry does not import the production contract module');
const module = new vm.SourceTextModule(code, {context,
  importModuleDynamically() { throw new Error('Dynamic contract imports are forbidden'); }});
if (module.dependencySpecifiers.length) throw new Error('Contract module must remain self-contained');
await module.link(() => { throw new Error('Contract imports are forbidden'); });
await module.evaluate({timeout: 1000});
if (typeof module.namespace.getDeploymentContract !== 'function')
  throw new Error('Missing production getDeploymentContract export');
context.__readContract = module.namespace.getDeploymentContract;
const result = vm.runInContext('JSON.stringify(__readContract())', context, {timeout: 1000});
process.stdout.write(JSON.stringify({contract: JSON.parse(result)}));
"""


class EntryScripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.paths = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'script' and attrs.get('type') == 'module':
            src = attrs.get('src', '')
            if not re.fullmatch(r'/assets/[A-Za-z0-9_-]+\.js', src):
                raise ValueError('unsafe or missing production module entry')
            self.paths.append(src)


def verify(root, contract):
    index = (root / 'index.html').read_bytes()
    parser = EntryScripts()
    parser.feed(index.decode('utf-8'))
    if len(parser.paths) != 1 or not re.fullmatch(r'/assets/index-[A-Za-z0-9_-]+\.js', parser.paths[0]):
        raise ValueError('unsupported or ambiguous production App entry layout')
    name = parser.paths[0]
    entry = (root / name.lstrip('/')).read_text(encoding='utf-8')
    # Require the named production getter import and a call of its local binding.
    # The VM independently confirms this is a real static module dependency.
    imports = re.findall(r'import\s*\{([^}]+)\}\s*from\s*["\'](\./deployment-contract-[A-Za-z0-9_-]+\.js)["\']', entry)
    bindings = []
    for names, specifier in imports:
        for item in names.split(','):
            match = re.fullmatch(r'\s*getDeploymentContract(?:\s+as\s+([A-Za-z_$][\w$]*))?\s*', item)
            if match:
                bindings.append((match.group(1) or 'getDeploymentContract', specifier))
    if len(bindings) != 1 or not re.search(r'(?<![\w$])' + re.escape(bindings[0][0]) + r'\s*\(', entry):
        raise ValueError('formal App must import and call the named production getter')
    reader_path = '/assets/' + bindings[0][1][2:]
    module_path = root / reader_path.lstrip('/')
    if not module_path.is_file():
        raise ValueError('missing production contract module')
    code = module_path.read_text(encoding='utf-8')
    node = shutil.which('node')
    if not node:
        raise ValueError('Node.js is required to verify the compiled production contract')
    try:
        result = subprocess.run(
            [node, '--experimental-vm-modules', '--input-type=module', '-e', NODE_PROBE],
            input=json.dumps({'entry': entry, 'code': code, 'panel': contract.get('panel'), 'readerSpecifier': bindings[0][1]}),
            text=True, capture_output=True, timeout=10, env={'PATH': os.environ.get('PATH', '')})
        if result.returncode:
            raise ValueError('production contract evaluation failed: ' + result.stderr[-2000:])
        actual = json.loads(result.stdout)['contract']
    except (subprocess.TimeoutExpired, OSError, KeyError, json.JSONDecodeError) as error:
        raise ValueError('production contract evaluation did not complete') from error
    if actual != contract or not isinstance(actual, dict):
        raise ValueError('formal App getter does not match named candidate/panel/gateway contract')
    return {'index_sha256': hashlib.sha256(index).hexdigest(),
            'entries': [{'path': name, 'sha256': hashlib.sha256(entry.encode()).hexdigest()}],
            'reader': {'path': reader_path, 'sha256': hashlib.sha256(code.encode()).hexdigest(),
                       'export': 'getDeploymentContract', 'app_import_binding': bindings[0][0]},
            'contract': actual, 'verification': 'compiled production getter evaluated; actual App browser readback still required'}
