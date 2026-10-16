"""Bind the actual index entry assets to the public deployment contract."""
import hashlib
import json
from html.parser import HTMLParser
import re


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
    # Vite's supported formal App layout has exactly one index entry. A
    # second metadata/harness module must never rescue a stale App entry.
    if len(parser.paths) != 1 or not re.fullmatch(r'/assets/index-[A-Za-z0-9_-]+\.js', parser.paths[0]):
        raise ValueError('unsupported or ambiguous production App entry layout')
    name = parser.paths[0]
    data = (root / name.lstrip('/')).read_text(encoding='utf-8')
    # Read complete JSON-compatible string literals emitted by Vite/esbuild.
    # Unsupported literal encodings fail closed instead of substring matching.
    literals = set()
    for token in re.findall(r'"(?:[^"\\]|\\.)*"', data):
        try:
            literals.add(json.loads(token))
        except ValueError:
            pass  # Other JS escape formats cannot satisfy this contract.
    expected = {contract[k] for k in ('candidate', 'panel', 'gateway')}
    if not expected.issubset(literals):
        raise ValueError('formal App entry does not match exact candidate/origins')
    return {'index_sha256': hashlib.sha256(index).hexdigest(),
            'entries': [{'path': name, 'sha256': hashlib.sha256(data.encode('utf-8')).hexdigest()}],
            'contract': contract}
