"""Finite synthetic gundeck characterization; run only in the owned runner."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import time

USER = '44444444-4444-4444-8444-444444444444'
ID = '00000000-0000-1000-8000-000000000001'
RESOURCE = 'http://127.0.0.1:16188/i/push/' + USER + '/fixture'


def redis_command(*parts):
    """Bounded RESP2 fixture access with certificate/hostname verification."""
    config = json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck']['redis']
    if config.get('insecureSkipVerifyTls'):
        raise ValueError('fixture requires verified TLS')
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ca = Path(config['tlsCa'])
    if not ca.is_absolute():
        ca = Path('/source/services/gundeck') / ca
    context.load_verify_locations(str(ca))
    host, port = config['host'], config['port']
    encoded = [str(p).encode() for p in parts]
    data = ('*' + str(len(parts)) + '\r\n').encode() + b''.join(
        ('$' + str(len(p)) + '\r\n').encode() + p + b'\r\n' for p in encoded)
    for _ in range(6):
        with socket.create_connection((host, port), timeout=5) as plain:
            with context.wrap_socket(plain, server_hostname=host) as conn:
                stream = conn.makefile('rb')
                password = os.environ.get('REDIS_PASSWORD')
                if password:
                    auth = [b'AUTH', password.encode()]
                    conn.sendall(b'*2\r\n' + b''.join(('$' + str(len(p)) + '\r\n').encode() + p + b'\r\n' for p in auth))
                    if stream.readline(128) != b'+OK\r\n':
                        raise ValueError('fixture Redis authentication failed')
                conn.sendall(data)
                line = stream.readline(65537)
                if len(line) > 65536 or not line.endswith(b'\r\n'):
                    raise ValueError('invalid bounded fixture RESP')
                if line.startswith(b'-MOVED '):
                    target = line.decode().strip().split()[2]
                    host, raw_port = target.rsplit(':', 1)
                    port = int(raw_port)
                    continue
                if line.startswith(b'-'):
                    raise ValueError('fixture Redis command failed')
                if line.startswith(b':'):
                    return int(line[1:-2])
                if line.startswith(b'$'):
                    length = int(line[1:-2])
                    if length == -1:
                        return None
                    if not 0 <= length <= 65536:
                        raise ValueError('fixture Redis value exceeds bound')
                    value = stream.read(length + 2)
                    if len(value) != length + 2 or value[-2:] != b'\r\n':
                        raise ValueError('truncated fixture Redis value')
                    return value[:-2].decode()
                raise ValueError('unsupported fixture RESP type')
    raise ValueError('too many fixture Redis redirects')


def capture(port):
    if os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
        raise ValueError('requires private owned environment')
    records = []
    redis_command('DEL', 'user:' + USER)

    def request(name, method, path, body=None, headers=None):
        headers = dict(headers or {}, **{'Request-Id': 'gundeck-synthetic-capture'})
        conn = http.client.HTTPConnection('127.0.0.1', port, timeout=5)
        try:
            conn.request(method, path, body, headers)
            response = conn.getresponse()
            raw = response.read(65537)
            if len(raw) > 65536:
                raise ValueError('response exceeded fixture bound')
            try:
                value = json.loads(raw)
            except (json.JSONDecodeError, UnicodeDecodeError):
                value = raw.decode()
            result = {'status': response.status, 'content_type': response.getheader('Content-Type'),
                      'allow': response.getheader('Allow'), 'location': response.getheader('Location'),
                      'body': value}
            # Validate generated time before the sole clock normalization.
            if isinstance(value, dict) and 'time' in value:
                from datetime import datetime, timezone
                stamp = datetime.fromisoformat(value['time'].replace('Z', '+00:00'))
                if abs((datetime.now(timezone.utc) - stamp).total_seconds()) > 10:
                    raise ValueError('server clock outside declared ten-second fixture bound')
                value['time'] = '<validated-current-UTC-second>'
            records.append({'name': name, 'observed': result})
            return result
        finally:
            conn.close()

    principal = {'Z-User': USER}
    for name, method, path, headers, body in [
        ('health', 'GET', '/i/status', {}, None),
        ('missing-principal', 'GET', '/v9/notifications', {}, None),
        ('invalid-principal', 'GET', '/v9/notifications', {'Z-User': 'invalid'}, None),
        ('empty-page', 'GET', '/v9/notifications', principal, None),
        ('missing-last', 'GET', '/v9/notifications/last', principal, None),
        ('missing-id', 'GET', '/v9/notifications/' + ID, principal, None),
        ('invalid-id', 'GET', '/v9/notifications/invalid', principal, None),
        ('invalid-since-v2', 'GET', '/v2/notifications?since=invalid', principal, None),
        ('invalid-since-v3', 'GET', '/v3/notifications?since=invalid', principal, None),
        ('non-v1-since', 'GET', '/v9/notifications?since=' + USER, principal, None),
        ('missing-since-v2', 'GET', '/v2/notifications?since=' + ID, principal, None),
        ('missing-since-v3', 'GET', '/v3/notifications?since=' + ID, principal, None),
        ('invalid-client', 'GET', '/v9/notifications?client=invalid', principal, None),
        ('size-too-small', 'GET', '/v9/notifications?size=99', principal, None),
        ('size-too-large', 'GET', '/v9/notifications?size=10001', principal, None),
        ('size-minimum', 'GET', '/v9/notifications?size=100', principal, None),
        ('time-before-v9', 'GET', '/v8/time', principal, None),
        ('time-v9', 'GET', '/v9/time', principal, None),
        ('empty-tokens', 'GET', '/v9/push/tokens', principal, None),
        ('delete-absent-token', 'DELETE', '/v9/push/tokens/synthetic-absent', principal, None),
        ('token-missing-connection', 'POST', '/v9/push/tokens', dict(principal, **{'Content-Type': 'application/json'}), '{}'),
        ('invalid-push-json', 'POST', '/i/push/v2', {'Content-Type': 'application/json'}, '{'),
        ('invalid-push-object', 'POST', '/i/push/v2', {'Content-Type': 'application/json'}, '{}'),
        ('empty-push-list', 'POST', '/i/push/v2', {'Content-Type': 'application/json'}, '[]'),
        ('empty-presence', 'GET', '/i/presences/' + USER, {}, None),
        ('invalid-presence-user', 'GET', '/i/presences/invalid', {}, None),
        ('presence-missing-ids', 'GET', '/i/presences', {}, None),
        ('presence-invalid-ids', 'GET', '/i/presences?ids=invalid', {}, None),
        ('invalid-presence-json', 'POST', '/i/presences', {}, '{'),
    ]:
        request(name, method, path, body, headers)
    presence = {'user_id': USER, 'device_id': 'fixture', 'client_id': 'abcd', 'resource': RESOURCE}
    request('presence-no-content-type', 'POST', '/i/presences', json.dumps(presence))
    listed = request('presence-list', 'GET', '/i/presences/' + USER)
    # created_at must be current milliseconds, normalized after validation.
    def validate_presence(result):
        for item in result['body']:
            stamp = item.get('created_at')
            if item['device_id'] == 'fixture':
                if not isinstance(stamp, int) or abs(time.time() * 1000 - stamp) > 10000:
                    raise ValueError('presence timestamp invalid')
                item['created_at'] = '<validated-current-milliseconds>'
    validate_presence(listed)
    listed = request('presence-multiple-users', 'GET', '/i/presences?ids=' + USER)
    validate_presence(listed)
    request('presence-delete-noop', 'DELETE', '/i/presences/' + USER + '/devices/fixture/cannons/fixture')
    listed = request('presence-after-delete', 'GET', '/i/presences/' + USER)
    validate_presence(listed)
    stored = json.loads(redis_command('HGET', 'user:' + USER, 'fixture'))
    if stored['r'] != RESOURCE or stored['c'] != 'abcd' or abs(time.time() * 1000 - stored['t']) > 10000:
        raise ValueError('stored presence shape differs')
    stored['t'] = '<validated-current-milliseconds>'
    records.append({'name': 'presence-stored-shape', 'observed': stored})
    ttl = redis_command('TTL', 'user:' + USER)
    if not 604790 <= ttl <= 604800:
        raise ValueError('presence TTL outside declared ten-second execution tolerance')
    records.append({'name': 'presence-ttl', 'observed': '<validated-seven-days-minus-execution>'})
    redis_command('HSET', 'user:' + USER, 'legacy@cannon', RESOURCE, 'bad', 'not-json')
    listed = request('legacy-and-malformed-presence', 'GET', '/i/presences/' + USER)
    validate_presence(listed)
    listed['body'].sort(key=lambda item: item['device_id'])
    redis_command('DEL', 'user:' + USER)
    request('presence-cleaned', 'GET', '/i/presences/' + USER)
    return {'schema_version': 1, 'kind': 'finite-http-presence-characterization', 'records': records,
            'script_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            'cleanup': 'reserved synthetic user Redis key deleted after capture; owned-stack volume cleanup remains final fallback'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--port', type=int)
    args = parser.parse_args()
    port = args.port or json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck']['gundeck']['port']
    print(json.dumps(capture(port), indent=2))
