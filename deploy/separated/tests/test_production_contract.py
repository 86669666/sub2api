import json
import pathlib
import tempfile
import unittest
from production_contract import verify


class ProductionContractTest(unittest.TestCase):
    def test_actual_entry_and_stale_bundle_negative(self):
        contract = {'candidate': 'a' * 40, 'panel': 'https://panel.example',
                    'gateway': 'https://gateway.example'}
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'assets').mkdir()
            (root / 'index.html').write_text('<script type="module" src="/assets/index-123.js"></script>')
            entry = root / 'assets/index-123.js'
            good = ';'.join(json.dumps(v) for v in contract.values())
            entry.write_text(good)
            self.assertEqual(verify(root, contract)['entries'][0]['path'], '/assets/index-123.js')
            # Correct manifests/harness/unreferenced assets cannot rescue an old App.
            (root / 'assets/harness.js').write_text(good)
            entry.write_text(good.replace('a' * 40, 'b' * 40))
            with self.assertRaises(ValueError):
                verify(root, contract)
            entry.write_text(good.replace(contract['gateway'], 'https://wrong.example'))
            with self.assertRaises(ValueError):
                verify(root, contract)

    def test_reject_prefix_origins_and_multiple_entries(self):
        contract = {'candidate': 'a' * 40, 'panel': 'https://panel.example',
                    'gateway': 'https://gateway.example'}
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'assets').mkdir()
            index = root / 'index.html'
            index.write_text('<script type="module" src="/assets/index-123.js"></script>')
            entry = root / 'assets/index-123.js'
            for key in ('panel', 'gateway'):
                wrong = {**contract, key: contract[key] + '.wrong.example'}
                entry.write_text(';'.join(json.dumps(v) for v in wrong.values()))
                with self.assertRaises(ValueError):
                    verify(root, contract)
            entry.write_text(';'.join(json.dumps(v) for v in {**contract, 'candidate': 'b' * 40}.values()))
            (root / 'assets/metadata.js').write_text(';'.join(json.dumps(v) for v in contract.values()))
            index.write_text(index.read_text() + '<script type="module" src="/assets/metadata.js"></script>')
            with self.assertRaises(ValueError):
                verify(root, contract)

    def test_reject_nonproduction_or_unsafe_entry(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for src in ['/__d0__/harness.js', '/assets/../app.js', 'https://outside.example/app.js']:
                (root / 'index.html').write_text(f'<script type="module" src="{src}"></script>')
                with self.assertRaises(ValueError):
                    verify(root, {})


if __name__ == '__main__':
    unittest.main()
