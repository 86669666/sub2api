"""Bind the actual index entry assets to the public deployment contract."""
import hashlib
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
    if not parser.paths:
        raise ValueError('production index has no module entry')
    expected = [contract[k].encode('utf-8') for k in ('candidate', 'panel', 'gateway')]
    assets = []
    matched = False
    for name in parser.paths:
        data = (root / name.lstrip('/')).read_bytes()
        assets.append({'path': name, 'sha256': hashlib.sha256(data).hexdigest()})
        matched |= all(value in data for value in expected)
    if not matched:
        raise ValueError('index-referenced production entry does not match candidate/origins')
    return {'index_sha256': hashlib.sha256(index).hexdigest(), 'entries': assets,
            'contract': contract}
