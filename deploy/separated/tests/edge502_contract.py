"""Pure verifier for the test-only upstream-close -> real Nginx 502 chain."""
import re
from datetime import datetime


def header(response, name):
    return [v for k, v in response['headers'] if k.lower() == name.lower()]


def verify_ack(ack, owner):
    assert ack['owner'] == owner
    assert ack['faultMode'] == 'close-before-headers'
    assert ack['status'] == 200 and ack['setupHang'] == '' and ack['wsRedirect'] is False
    assert type(ack['revision']) is int and ack['revision'] == ack['previousRevision'] + 1
    assert type(ack['sequence']) is int and ack['sequence'] >= 0


def verify(item, owner, expected_targets):
    ack = item['ack']; verify_ack(ack, owner)
    name = item['name']; assert name in ('gateway', 'panel')
    path = '/v1/usage' if name == 'gateway' else '/setup/status'
    assert item['path'] == path
    assert item['identity_before'] == item['identity_after'] == expected_targets
    before, after = item['before'], item['after']
    assert len(after) == len(before) + 1 and after[:-1] == before
    row = after[-1]
    assert row['method'] == 'GET' and row['url'] == path
    assert row['revision'] == ack['revision'] and row['sequence'] == ack['sequence'] + 1
    assert row['responseId'] == f"d0-r{ack['revision']}-s{ack['sequence']+1}"
    assert row['status'] is None and row['configuredStatus'] == 200
    fault = row['fault']
    assert fault['owner'] == owner and fault['mode'] == 'close-before-headers'
    assert fault['headersSent'] is False and fault['httpStatus'] is None and fault['responseBytes'] == 0
    assert fault['closeRequested'] is True and fault['closeCompleted'] is True
    requested = datetime.fromisoformat(fault['requestedAt'].replace('Z', '+00:00'))
    closed = datetime.fromisoformat(fault['closedAt'].replace('Z', '+00:00'))
    assert requested.tzinfo is not None and closed.tzinfo is not None and requested <= closed
    assert fault['localAddress'] == expected_targets['mock']['ip'] and fault['localPort'] == 8080
    assert fault['remoteAddress'] == expected_targets[name]['ip']
    assert type(fault['remotePort']) is int and 0 < fault['remotePort'] <= 65535
    assert fault['socketId'] == f"{fault['localAddress']}:{fault['localPort']}<-{fault['remoteAddress']}:{fault['remotePort']}"
    response = item['response']
    assert response['status'] == 502 and 0 <= item['elapsed_seconds'] < 10
    assert not header(response, 'X-Request-Id') and not header(response, 'Access-Control-Allow-Credentials')
    assert header(response, 'Access-Control-Allow-Origin') == (['http://127.0.0.1:8088'] if name == 'gateway' else [])
    if name == 'gateway':
        assert header(response, 'Vary') == ['Origin, Access-Control-Request-Method, Access-Control-Request-Headers']
    else:
        assert header(response, 'Cache-Control') == ['no-store']
    # Delta captured around this single request, with exact upstream address/path.
    lines = item['nginx_log_delta'].splitlines()
    request = f'GET {path} HTTP/1.1'
    errors = [x for x in lines if f'request: "{request}"' in x and
              ('upstream prematurely closed connection' in x or 'recv() failed' in x) and
              'while reading response header from upstream' in x and
              f'upstream: "http://{expected_targets["mock"]["ip"]}:8080{path}"' in x]
    access = [x for x in lines if re.search(r'"' + re.escape(request) + r'" 502 \d+ ', x)]
    assert len(errors) == len(access) == 1
    return True
