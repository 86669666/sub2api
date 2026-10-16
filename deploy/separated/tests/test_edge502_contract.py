import copy
import unittest
from edge502_contract import verify


def sample(name='gateway'):
    targets = {'mock': {'ip': '172.18.0.2', 'endpoint_id': 'mock'},
               'gateway': {'ip': '172.18.0.3', 'endpoint_id': 'gateway'},
               'panel': {'ip': '172.18.0.4', 'endpoint_id': 'panel'}}
    path = '/v1/usage' if name == 'gateway' else '/setup/status'
    ip = targets[name]['ip']
    fault = dict(owner='owned', mode='close-before-headers', headersSent=False,
                 httpStatus=None, responseBytes=0, closeRequested=True, closeCompleted=True,
                 requestedAt='2026-10-16T00:00:00Z', closedAt='2026-10-16T00:00:01Z',
                 localAddress='172.18.0.2', localPort=8080, remoteAddress=ip, remotePort=12345,
                 socketId=f'172.18.0.2:8080<-{ip}:12345')
    headers = ([('Access-Control-Allow-Origin', 'http://127.0.0.1:8088'),
                ('Vary', 'Origin, Access-Control-Request-Method, Access-Control-Request-Headers')]
               if name == 'gateway' else [('Cache-Control', 'no-store')])
    item = dict(name=name, path=path, identity_before=copy.deepcopy(targets), identity_after=copy.deepcopy(targets),
                ack=dict(owner='owned', faultMode='close-before-headers', status=200, setupHang='', wsRedirect=False,
                         previousRevision=0, revision=1, sequence=0), before=[],
                after=[dict(method='GET', url=path, revision=1, sequence=1, responseId='d0-r1-s1', status=None, configuredStatus=200, fault=fault)],
                response=dict(status=502, headers=headers), elapsed_seconds=.02,
                nginx_log_delta=f'upstream prematurely closed connection while reading response header from upstream, request: "GET {path} HTTP/1.1", upstream: "http://172.18.0.2:8080{path}"\n127.0.0.1 - - [date] "GET {path} HTTP/1.1" 502 157 "-" "-"\n')
    return item, targets


class EdgeContract(unittest.TestCase):
    def test_both_edges_positive(self):
        for name in ['gateway', 'panel']:
            item, targets = sample(name)
            self.assertTrue(verify(item, 'owned', targets))

    def test_false_evidence_rejected(self):
        mutations = [
            lambda x: x['ack'].update(owner='other'),
            lambda x: x['ack'].update(faultMode='none'),
            lambda x: x['ack'].update(status=502),
            lambda x: x['ack'].update(revision=5),
            lambda x: x['ack'].update(setupHang='body'),
            lambda x: x['identity_after']['mock'].update(endpoint_id='changed'),
            lambda x: x['identity_before']['gateway'].update(ip='wrong'),
            lambda x: x.update(after=[]),
            lambda x: x['after'].append(copy.deepcopy(x['after'][0])),
            lambda x: x['after'][0].update(revision=2),
            lambda x: x['after'][0].update(sequence=2),
            lambda x: x['after'][0].update(url='/health'),
            lambda x: x['after'][0].update(status=502),
            lambda x: x['after'][0]['fault'].update(headersSent=True),
            lambda x: x['after'][0]['fault'].update(responseBytes=157),
            lambda x: x['after'][0]['fault'].update(httpStatus=502),
            lambda x: x['after'][0]['fault'].update(closeCompleted=False),
            lambda x: x['after'][0]['fault'].update(owner='other'),
            lambda x: x['after'][0]['fault'].update(remoteAddress='wrong'),
            lambda x: x['after'][0]['fault'].update(socketId='invented'),
            lambda x: x['response'].update(status=504),
            lambda x: x['response'].update(status=499),
            lambda x: x.update(elapsed_seconds=10),
            lambda x: x['response'].update(headers=[]),
            lambda x: x['response']['headers'].append(('Access-Control-Allow-Credentials', 'true')),
            lambda x: x.update(nginx_log_delta=''),
            lambda x: x.update(nginx_log_delta=x['nginx_log_delta'].replace('172.18.0.2','172.18.0.99')),
            lambda x: x.update(nginx_log_delta=x['nginx_log_delta']*2),
        ]
        for index, mutate in enumerate(mutations):
            with self.subTest(index=index):
                item, targets = sample(); mutate(item)
                with self.assertRaises((AssertionError, KeyError)): verify(item, 'owned', targets)

    def test_panel_cache_control_exactly_one(self):
        for headers in [[], [('Cache-Control','public')], [('Cache-Control','no-store')]*2]:
            item, targets = sample('panel');item['response']['headers'] = headers
            with self.assertRaises(AssertionError): verify(item, 'owned', targets)


if __name__ == '__main__':
    unittest.main()
