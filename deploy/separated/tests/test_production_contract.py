import json
import pathlib
import tempfile
import unittest
from production_contract import verify


class ProductionContractTest(unittest.TestCase):
    contract = {'format': 1, 'mode': 'separated', 'candidate': 'a' * 40,
                'panel': 'https://panel.example', 'gateway': 'https://gateway.example'}

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / 'assets').mkdir()
        self.index = self.root / 'index.html'
        self.index.write_text('<script type="module" src="/assets/index-123.js"></script>')
        self.entry = self.root / 'assets/index-123.js'
        self.entry.write_text('import {getDeploymentContract as read} from "./deployment-contract-123.js";'
                              'const appContract = read();')
        self.reader = self.root / 'assets/deployment-contract-123.js'
        self.write_reader(self.contract)

    def write_reader(self, actual, extra=''):
        self.reader.write_text('export function getDeploymentContract(){return ' + json.dumps(actual) + ';}' + extra)

    def test_actual_entry_and_stale_bundle_negative(self):
        self.assertEqual(verify(self.root, self.contract)['contract'], self.contract)
        (self.root / 'assets/harness.js').write_text(json.dumps(self.contract))
        self.write_reader({**self.contract, 'candidate': 'b' * 40})
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)
        self.write_reader({**self.contract, 'gateway': 'https://wrong.example'})
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)

    def test_reject_prefix_origins_and_multiple_entries(self):
        for key in ('panel', 'gateway'):
            with self.subTest(key=key):
                self.write_reader({**self.contract, key: self.contract[key] + '.wrong.example'})
                with self.assertRaises(ValueError):
                    verify(self.root, self.contract)
        self.write_reader({**self.contract, 'candidate': 'b' * 40})
        (self.root / 'assets/metadata.js').write_text(json.dumps(self.contract))
        self.index.write_text(self.index.read_text() + '<script type="module" src="/assets/metadata.js"></script>')
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)

    def test_reject_swapped_named_origins(self):
        self.write_reader({**self.contract, 'panel': self.contract['gateway'], 'gateway': self.contract['panel']})
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)

    def test_stale_getter_cannot_be_rescued_by_metadata_in_either_chunk(self):
        metadata = 'export const metadata=' + json.dumps(self.contract) + ';'
        self.entry.write_text(self.entry.read_text() + metadata)
        self.write_reader({**self.contract, 'candidate': 'b' * 40}, metadata)
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)

    def test_old_inline_app_with_correct_metadata_is_rejected(self):
        # Preserve the exact shape of the independent review's second counterexample.
        self.entry.write_text('export const appContract=' + json.dumps({**self.contract, 'candidate': 'b' * 40}) +
                              ';export const metadata=' + json.dumps(self.contract) + ';')
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)

    def test_reject_nonproduction_or_unsafe_entry(self):
        for src in ['/__d0__/harness.js', '/assets/../app.js', 'https://outside.example/app.js']:
            with self.subTest(src=src):
                self.index.write_text(f'<script type="module" src="{src}"></script>')
                with self.assertRaises(ValueError):
                    verify(self.root, self.contract)

    def test_reader_must_be_a_real_named_import_used_by_app(self):
        for entry in [
            'import "./deployment-contract-123.js";',
            'import {getDeploymentContract as read} from "./deployment-contract-123.js"; const metadata = 1;',
            'const pretend = `import {getDeploymentContract as read} from "./deployment-contract-123.js"; read();`;'
        ]:
            with self.subTest(entry=entry):
                self.entry.write_text(entry)
                with self.assertRaises(ValueError):
                    verify(self.root, self.contract)

    def test_missing_export_and_imports_fail_closed(self):
        for source in ['export const metadata=' + json.dumps(self.contract) + ';',
                       'import "node:fs"; export function getDeploymentContract(){return {};}',
                       'export function getDeploymentContract(){return import("node:fs");}']:
            with self.subTest(source=source):
                self.reader.write_text(source)
                with self.assertRaises(ValueError):
                    verify(self.root, self.contract)

    def test_nonterminating_getter_has_a_bounded_failure(self):
        self.reader.write_text('export function getDeploymentContract(){while(true){}}')
        with self.assertRaises(ValueError):
            verify(self.root, self.contract)


if __name__ == '__main__':
    unittest.main()
