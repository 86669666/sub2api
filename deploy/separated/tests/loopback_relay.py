"""Task-owned CI byte relay. No HTTP parsing, payload logging, or target discovery."""
import argparse
import asyncio
import ipaddress
import json
import os
from pathlib import Path
import signal
import sys

PORTS = {'panel': (8088, 80), 'gateway': (8089, 80), 'mock': (8090, 8080)}
LIMIT = 128
BLOCK = 65536


def identities(owner, network, containers, image_ids, previous=None, stopped_mock=False):
    """Validate inspect output; return immutable identity used by the child."""
    if (network.get('Name') != owner or network.get('Driver') != 'bridge'
            or network.get('Internal') is not True
            or network.get('Labels', {}).get('sub2api.d0.owner') != owner):
        raise ValueError('network ownership or isolation mismatch')
    network_id = network['Id']
    if not network_id or set(containers) != set(PORTS) or set(image_ids) != set(PORTS):
        raise ValueError('incomplete identity input')
    targets = {}
    for name, (listen, target_port) in PORTS.items():
        c = containers[name]
        state = c['State']
        if (c['Name'] != '/' + owner + '-' + name
                or c['Config'].get('Labels', {}).get('sub2api.d0.owner') != owner
                or c['Image'] != image_ids[name]
                or state.get('Restarting') is not False
                or c['HostConfig'].get('RestartPolicy', {}).get('Name') not in ('no', '')
                or c['HostConfig'].get('NetworkMode') != owner):
            raise ValueError('container ownership/state/image mismatch')
        if stopped_mock and name == 'mock':
            if state.get('Running') is not False or previous is None or c['Id'] != previous[name]['container_id']:
                raise ValueError('unexpected stopped mock identity')
            # Docker removes the stopped endpoint. The frozen relay target is
            # retained only for the deliberate, final Nginx edge-502 probe.
            if set(c['NetworkSettings']['Networks']) - {owner}:
                raise ValueError('stopped mock gained another network')
            targets[name] = previous[name]
            continue
        if state.get('Running') is not True:
            raise ValueError('target not running')
        networks = c['NetworkSettings']['Networks']
        if set(networks) != {owner}:
            raise ValueError('additional or missing container network')
        endpoint = networks[owner]
        ip = ipaddress.IPv4Address(endpoint['IPAddress'])
        member = network.get('Containers', {}).get(c['Id'], {})
        if (ip.is_loopback or ip.is_unspecified or ip.is_multicast or ip.is_link_local
                or endpoint['NetworkID'] != network_id or not endpoint.get('EndpointID')
                or member.get('Name') != owner + '-' + name
                or member.get('EndpointID') != endpoint['EndpointID']
                or member.get('IPv4Address', '').split('/')[0] != str(ip)):
            raise ValueError('network endpoint/IP mismatch')
        if not any(ip in ipaddress.IPv4Network(x['Subnet']) for x in network['IPAM']['Config'] if ':' not in x['Subnet']):
            raise ValueError('IP outside owned network')
        targets[name] = dict(container_id=c['Id'], image_id=c['Image'], network_id=network_id,
                             endpoint_id=endpoint['EndpointID'], ip=str(ip), listen=listen, port=target_port)
    if previous is not None and targets != previous:
        raise ValueError('frozen target drift')
    return targets


class Relay:
    def __init__(self, targets):
        self.targets = targets
        self.servers = []
        self.tasks = set()
        self.writers = set()
        self.errors = 0
        self.closing = False

    async def start(self):
        try:
            for target in self.targets.values():
                server = await asyncio.start_server(
                    lambda r, w, t=target: self.accept(r, w, t),
                    '127.0.0.1', target['listen'], family=2,
                    # Rebind our previous TIME_WAIT sockets, never share a live listener.
                    reuse_address=True, reuse_port=False, start_serving=False,
                    limit=BLOCK)
                self.servers.append(server)
            # No listener accepts until ALL binds have succeeded.
            for server in self.servers:
                await server.start_serving()
        except BaseException:
            await self.close()
            raise

    def accept(self, reader, writer, target):
        if self.closing or len(self.tasks) >= LIMIT:
            writer.close()
            return
        self.writers.add(writer)
        task = asyncio.create_task(self.forward(reader, writer, target))
        self.tasks.add(task)
        task.add_done_callback(self.tasks.discard)

    async def forward(self, reader, writer, target):
        self.writers.add(writer)
        upstream = None
        pumps = []
        try:
            remote, upstream = await asyncio.wait_for(
                asyncio.open_connection(target['ip'], target['port'], family=2, limit=BLOCK), 5)
            self.writers.add(upstream)
            async def pump(source, dest):
                while True:
                    block = await source.read(BLOCK)
                    if not block:
                        dest.write_eof()
                        await dest.drain()
                        return
                    dest.write(block)
                    await dest.drain()
            pumps = [asyncio.create_task(pump(reader, upstream)),
                     asyncio.create_task(pump(remote, writer))]
            # EOF is a half close; wait for the opposite direction's tail.
            await asyncio.gather(*pumps)
        except (OSError, asyncio.TimeoutError, RuntimeError):
            self.errors += 1
        finally:
            for task in pumps:
                task.cancel()
            await asyncio.gather(*pumps, return_exceptions=True)
            for stream in (writer, upstream):
                if stream is not None:
                    stream.close()
                    try:
                        await asyncio.wait_for(stream.wait_closed(), 2)
                    except (OSError, asyncio.TimeoutError):
                        pass
                    self.writers.discard(stream)

    async def close(self):
        # Python 3.12 Server.wait_closed also waits for accepted transports.
        # Stop accepts, close those transports/tasks, THEN wait for the server.
        self.closing = True
        for server in self.servers:
            server.close()
        tasks, writers = list(self.tasks), list(self.writers)
        for task in tasks:
            task.cancel()
        for writer in writers:
            writer.close()

        async def finish():
            await asyncio.gather(*tasks, return_exceptions=True)
            await asyncio.gather(*(w.wait_closed() for w in writers), return_exceptions=True)
            await asyncio.gather(*(s.wait_closed() for s in self.servers))
        # One total deadline, not a separate allowance for each connection.
        await asyncio.wait_for(finish(), 8)
        self.servers.clear()
        self.writers.clear()


async def child(config, ready, receipt):
    # Fail closed: this executable may only listen in the ephemeral CI job.
    if os.environ.get('GITHUB_ACTIONS') != 'true' or os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted' or sys.platform != 'linux':
        raise RuntimeError('relay requires authorized ephemeral Linux CI')
    targets = json.loads(config.read_text())
    if set(targets) != set(PORTS):
        raise ValueError('fixed targets required')
    for name, target in targets.items():
        if (target['listen'], target['port']) != PORTS[name]:
            raise ValueError('fixed port mismatch')
        ipaddress.IPv4Address(target['ip'])
    relay = Relay(targets)
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    reason = []
    def stopping(why):
        reason.append(why)
        stop.set()
    # Parent holds the pipe's write end. SIGKILL/crash closes it too.
    os.set_blocking(sys.stdin.fileno(), False)
    def parent_pipe():
        if os.read(sys.stdin.fileno(), 4096) == b'':
            loop.remove_reader(sys.stdin.fileno())
            stopping('parent_pipe_eof')
    loop.add_reader(sys.stdin.fileno(), parent_pipe)
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, stopping, sig.name)
    try:
        await relay.start()
        temp = ready.with_suffix('.tmp')
        temp.write_text(json.dumps({'pid': os.getpid(), 'targets': targets, 'listeners': ['127.0.0.1:' + str(t['listen']) for t in targets.values()]}))
        os.replace(temp, ready)
        await stop.wait()
    finally:
        loop.remove_reader(sys.stdin.fileno())
        cleanup_error = None
        try:
            await relay.close()
        except BaseException as error:
            cleanup_error = error
        receipt.write_text(json.dumps({'pid': os.getpid(), 'reason': reason, 'active_tasks': len(relay.tasks),
                                       'active_writers': len(relay.writers), 'listeners_closed': not relay.servers,
                                       'connection_errors': relay.errors,
                                       'cleanup_error': type(cleanup_error).__name__ if cleanup_error else None}))
        if cleanup_error is not None:
            raise cleanup_error


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--ready', type=Path, required=True)
    parser.add_argument('--receipt', type=Path, required=True)
    args = parser.parse_args()
    asyncio.run(child(args.config, args.ready, args.receipt))
