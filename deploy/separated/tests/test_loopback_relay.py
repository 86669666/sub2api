import asyncio
import copy
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest
from loopback_relay import identities, Relay, PORTS


def fixture():
    owner = 'sub2api-d0-test'
    net = {'Name': owner, 'Id': 'network-id', 'Driver': 'bridge', 'Internal': True,
           'Labels': {'sub2api.d0.owner': owner}, 'Containers': {},
           'IPAM': {'Config': [{'Subnet': '172.18.0.0/16'}]}}
    containers, images = {}, {}
    for n, name in enumerate(PORTS, 2):
        ip = f'172.18.0.{n}'
        cid, eid = name + '-id', name + '-endpoint'
        images[name] = name + '-image'
        containers[name] = {'Id': cid, 'Name': '/' + owner + '-' + name, 'Image': images[name],
            'Config': {'Labels': {'sub2api.d0.owner': owner}}, 'State': {'Running': True, 'Restarting': False},
            'HostConfig': {'NetworkMode': owner, 'RestartPolicy': {'Name': 'no'}},
            'NetworkSettings': {'Networks': {owner: {'NetworkID': net['Id'], 'EndpointID': eid, 'IPAddress': ip}}}}
        net['Containers'][cid] = {'Name': owner + '-' + name, 'EndpointID': eid, 'IPv4Address': ip + '/16'}
    return owner, net, containers, images


class IdentityTests(unittest.TestCase):
    def test_frozen_identity_and_explicit_mock_stop(self):
        args = fixture()
        fixed = identities(*args)
        self.assertEqual(fixed, identities(*args, previous=fixed))
        args[2]['mock']['State']['Running'] = False
        args[2]['mock']['NetworkSettings']['Networks'] = {}
        with self.assertRaises(ValueError):
            identities(*args, previous=fixed)
        self.assertEqual(fixed, identities(*args, previous=fixed, stopped_mock=True))

    def test_reject_owner_network_ip_image_and_drift(self):
        changes = [
            lambda n, c: n.update(Internal=False),
            lambda n, c: n['Labels'].update({'sub2api.d0.owner': 'foreign'}),
            lambda n, c: c['panel']['Config']['Labels'].update({'sub2api.d0.owner': 'foreign'}),
            lambda n, c: c['panel']['NetworkSettings']['Networks'].update(other={}),
            lambda n, c: c['panel']['NetworkSettings']['Networks'][n['Name']].update(IPAddress='10.10.88.120'),
            lambda n, c: c['panel']['NetworkSettings']['Networks'][n['Name']].update(NetworkID='other'),
            lambda n, c: c['panel'].update(Image='other'),
            lambda n, c: c['panel']['State'].update(Restarting=True),
        ]
        for change in changes:
            args = fixture()
            with self.subTest(change=change):
                change(args[1], args[2])
                with self.assertRaises(ValueError):
                    identities(*args)
        args = fixture()
        fixed = identities(*args)
        stale = copy.deepcopy(fixed)
        stale['panel']['container_id'] = 'replaced'
        with self.assertRaises(ValueError):
            identities(*args, previous=stale)


NETWORK_CI = (sys.platform == 'linux' and os.environ.get('GITHUB_ACTIONS') == 'true'
              and os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


@unittest.skipUnless(NETWORK_CI, 'network tests only in authorized ephemeral CI')
class StreamTests(unittest.IsolatedAsyncioTestCase):
    async def test_partial_bind_rolls_back_without_harming_occupant(self):
        occupied = await asyncio.start_server(lambda r, w: w.close(), '127.0.0.1', 0)
        first = free_port()
        second = occupied.sockets[0].getsockname()[1]
        relay = Relay({'one': {'ip': '127.0.0.1', 'port': 1, 'listen': first},
                       'two': {'ip': '127.0.0.1', 'port': 1, 'listen': second}})
        try:
            with self.assertRaises(OSError):
                await relay.start()
            self.assertEqual(relay.servers, [])
            with socket.socket() as sock:
                sock.bind(('127.0.0.1', first))
            r, w = await asyncio.open_connection('127.0.0.1', second)
            w.close()
            await w.wait_closed()
        finally:
            occupied.close()
            await occupied.wait_closed()

    async def test_raw_stream_backpressure_halfclose_and_reverse_tail(self):
        # More than stream limits; slow receiver and a response AFTER EOF.
        payload = bytes(range(256)) * 32768
        completed = asyncio.get_running_loop().create_future()
        async def receive(reader, writer):
            digest = hashlib.sha256()
            try:
                while block := await reader.read(4096):
                    digest.update(block)
                    await asyncio.sleep(.0001)
                writer.write(digest.digest() + b'\x00\xffTAIL')
                await writer.drain()
                writer.write_eof()
                completed.set_result(True)
            except Exception as error:
                completed.set_exception(error)
            finally:
                writer.close()
                await writer.wait_closed()
        server = await asyncio.start_server(receive, '127.0.0.1', 0)
        relay = Relay({'test': {'ip': '127.0.0.1', 'port': server.sockets[0].getsockname()[1], 'listen': free_port()}})
        try:
            await relay.start()
            reader, writer = await asyncio.open_connection('127.0.0.1', relay.targets['test']['listen'])
            for offset in range(0, len(payload), 65536):
                writer.write(payload[offset:offset+65536])
                await writer.drain()
            writer.write_eof()
            self.assertEqual(await asyncio.wait_for(reader.read(), 20), hashlib.sha256(payload).digest() + b'\x00\xffTAIL')
            await completed
            writer.close()
            await writer.wait_closed()
        finally:
            await relay.close()
            server.close()
            await server.wait_closed()
        self.assertEqual(len(relay.tasks), 0)
        self.assertEqual(len(relay.writers), 0)

    async def test_refused_target_and_cancel_cleanup(self):
        relay = Relay({'test': {'ip': '127.0.0.1', 'port': free_port(), 'listen': free_port()}})
        await relay.start()
        try:
            reader, writer = await asyncio.open_connection('127.0.0.1', relay.targets['test']['listen'])
            self.assertEqual(await asyncio.wait_for(reader.read(), 10), b'')
            writer.close()
            await writer.wait_closed()
        finally:
            await relay.close()
        self.assertGreater(relay.errors, 0)
        self.assertFalse(relay.tasks)

    async def test_cancel_active_connection_releases_tasks_and_sockets(self):
        connected = asyncio.Event()
        finished = asyncio.Event()
        async def hold(reader, writer):
            connected.set()
            await reader.read()
            writer.close()
            await writer.wait_closed()
            finished.set()
        server = await asyncio.start_server(hold, '127.0.0.1', 0)
        relay = Relay({'test': {'ip': '127.0.0.1', 'port': server.sockets[0].getsockname()[1], 'listen': free_port()}})
        try:
            await relay.start()
            reader, writer = await asyncio.open_connection('127.0.0.1', relay.targets['test']['listen'])
            await asyncio.wait_for(connected.wait(), 5)
            await relay.close()
            self.assertEqual(await asyncio.wait_for(reader.read(), 5), b'')
            await asyncio.wait_for(finished.wait(), 5)
            self.assertFalse(relay.tasks)
            self.assertFalse(relay.writers)
            writer.close()
            await writer.wait_closed()
        finally:
            await relay.close()
            server.close()
            await server.wait_closed()

    async def test_parent_pipe_eof_and_sigterm_close_listeners(self):
        for action in ('parent_eof', 'sigterm'):
            with self.subTest(action=action), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                config, ready, receipt = [root / n for n in ('config.json', 'ready.json', 'receipt.json')]
                config.write_text(json.dumps({name: {'ip': '127.0.0.1', 'listen': ports[0], 'port': ports[1]} for name, ports in PORTS.items()}))
                child = subprocess.Popen([sys.executable, str(Path(__file__).with_name('loopback_relay.py')),
                    '--config', str(config), '--ready', str(ready), '--receipt', str(receipt)],
                    stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                    env={k: os.environ[k] for k in ('PATH', 'GITHUB_ACTIONS', 'RUNNER_ENVIRONMENT')})
                try:
                    for _ in range(100):
                        if ready.exists() or child.poll() is not None:
                            break
                        await asyncio.sleep(.05)
                    self.assertTrue(ready.exists(), 'child failed before all binds')
                    if action == 'parent_eof':
                        child.stdin.close()
                    else:
                        child.terminate()
                    await asyncio.wait_for(asyncio.to_thread(child.wait), 10)
                    self.assertEqual(child.returncode, 0, child.stderr.read().decode())
                    evidence = json.loads(receipt.read_text())
                    self.assertEqual(evidence['active_tasks'], 0)
                    self.assertEqual(evidence['active_writers'], 0)
                    self.assertTrue(evidence['listeners_closed'])
                    self.assertIn('parent_pipe_eof' if action == 'parent_eof' else 'SIGTERM', evidence['reason'])
                    for listen, _ in PORTS.values():
                        with socket.socket() as sock:
                            sock.bind(('127.0.0.1', listen))
                finally:
                    if child.poll() is None:
                        child.kill()
                    await asyncio.to_thread(child.wait)
                    child.stderr.close()
                    if not child.stdin.closed:
                        child.stdin.close()


if __name__ == '__main__':
    unittest.main()
