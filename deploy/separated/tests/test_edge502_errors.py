"""Exercise the real runner function via AST; do not execute its CI-only entry."""
import ast
import datetime
import json
from pathlib import Path
import time
import types
import unittest


class Sink:
    def __init__(self, failure):
        self.failure = failure
        self.writes = []

    def __truediv__(self, _):
        return self

    def write_text(self, value):
        if self.failure:
            raise self.failure
        self.writes.append(value)


class PrimaryErrorTests(unittest.TestCase):
    def scenario(self, primary, write_error):
        tree = ast.parse(Path(__file__).with_name('ci-run.py').read_text())
        function = [x for x in tree.body if isinstance(x, ast.FunctionDef) and x.name == 'edge_probe']
        self.assertEqual(len(function), 1)
        module = ast.Module(body=function, type_ignores=[])
        receipt, sink = {}, Sink(write_error)
        def request(*args, **kwargs):
            if primary:
                raise primary
            return {'status': 502}
        env = dict(datetime=datetime, time=time, json=json, receipt=receipt,
                   run_id='owned', targets={}, out=sink, inspect_targets=lambda: {},
                   rows=lambda: [{'fault': {'closeCompleted': True}}], request=request,
                   run=lambda *args, **kwargs: types.SimpleNamespace(stdout='"GET /v1/usage HTTP/1.1" 502 157 '),
                   verify_edge502=lambda *args: True, check=lambda *args: None)
        exec(compile(module, 'real-edge-probe-with-memory-io', 'exec'), env)
        caught = None
        try:
            env['edge_probe']('gateway', 8089, '/v1/usage', {}, [])
        except BaseException as error:
            caught = error
        return caught, receipt['edge502'][0], sink

    def test_primary_failure_successful_write(self):
        primary = TimeoutError('probe timeout')
        error, item, sink = self.scenario(primary, None)
        self.assertIs(error, primary)
        self.assertEqual(item['error'], repr(primary))
        self.assertEqual(len(sink.writes), 1)
        self.assertNotIn('evidence_write_error', item)

    def test_primary_failure_failed_write_preserves_same_exception(self):
        primary, secondary = TimeoutError('probe timeout'), OSError('write failed')
        error, item, sink = self.scenario(primary, secondary)
        self.assertIs(error, primary)
        self.assertEqual(item['error'], repr(primary))
        self.assertEqual(item['evidence_write_error'], repr(secondary))
        self.assertEqual(sink.writes, [])

    def test_successful_probe_failed_write_fails_closed(self):
        secondary = OSError('write failed')
        error, item, _ = self.scenario(None, secondary)
        self.assertIs(error, secondary)
        self.assertNotIn('error', item)
        self.assertEqual(item['evidence_write_error'], repr(secondary))

    def test_successful_probe_successful_write(self):
        error, item, sink = self.scenario(None, None)
        self.assertIsNone(error)
        self.assertNotIn('evidence_write_error', item)
        self.assertEqual(len(sink.writes), 1)


if __name__ == '__main__':
    unittest.main()
