#!/usr/bin/env python3
"""Owned ActiveMQ fixture and bounded STOMP/JMX observations; not an API oracle."""
import base64
import contextlib
import http.client
import json
from pathlib import Path
import socket
import time
import uuid

IMAGE = 'apache/activemq@sha256:1cacfbd9c08ad437c5e1177a9f1217e7c7ada62a64065128acd1a81866a03711'
IMAGE_ID = 'sha256:b9972c335fae8deb648f3f048622aaf3540c82d5c42741c9e6dad43af1e18180'
USER = 'admin'
PASSWORD = 'Synthetic-owned-STOMP-password!'
QUEUE = '/queue/integration-brig-events-internal'
POISON = b'{"type":"synthetic-invalid-internal-event"}'

CONFIG = '''<beans xmlns="http://www.springframework.org/schema/beans"
 xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
 xsi:schemaLocation="http://www.springframework.org/schema/beans http://www.springframework.org/schema/beans/spring-beans.xsd
 http://activemq.apache.org/schema/core http://activemq.apache.org/schema/core/activemq-core.xsd">
 <bean class="org.springframework.beans.factory.config.PropertyPlaceholderConfigurer"><property name="locations"><value>file:/tmp/base/conf/credentials.properties</value></property></bean>
 <broker xmlns="http://activemq.apache.org/schema/core" brokerName="wire-owned" dataDirectory="${activemq.data}">
  <managementContext><managementContext createConnector="false"/></managementContext>
  <persistenceAdapter><kahaDB directory="${activemq.data}/kahadb"/></persistenceAdapter>
  <plugins><jaasAuthenticationPlugin configuration="activemq"/></plugins>
  <systemUsage><systemUsage>
   <memoryUsage><memoryUsage limit="64 mb"/></memoryUsage>
   <storeUsage><storeUsage limit="64 mb"/></storeUsage>
   <tempUsage><tempUsage limit="64 mb"/></tempUsage>
  </systemUsage></systemUsage>
  <transportConnectors><transportConnector name="stomp" uri="stomp://0.0.0.0:61613?maximumConnections=32&amp;wireFormat.maxFrameSize=4194304"/></transportConnectors>
  <shutdownHooks><bean xmlns="http://www.springframework.org/schema/beans" class="org.apache.activemq.hooks.SpringContextHook"/></shutdownHooks>
 </broker>
 <import resource="jetty-spring.xml"/>
</beans>'''

# All changes below affect this disposable broker only. JMX remains read-only,
# POST-only and authenticated; management has no host-facing port or connector.
START = '''set -eu
mkdir -p /tmp/base/conf /tmp/base/data /tmp/base/tmp
cp -R /opt/apache-activemq/conf/. /tmp/base/conf/
cp /fixtures/activemq.xml /tmp/base/conf/activemq.xml
cp /fixtures/stomp-users.properties /tmp/base/conf/users.properties
sed -i 's@<Item>127.0.0.1</Item>@<Item>0.0.0.0/0</Item>@' /tmp/base/conf/jetty/jetty-security.xml
echo 'jetty.http.host=0.0.0.0' >> /tmp/base/conf/jetty-spring.properties
exec /opt/java/openjdk/bin/java -Xms64m -Xmx256m \
 -Dactivemq.home=/opt/apache-activemq -Dactivemq.base=/tmp/base \
 -Dactivemq.conf=/tmp/base/conf -Dactivemq.data=/tmp/base/data \
 -Djolokia.conf=file:/tmp/base/conf/jolokia-access.xml \
 -Djava.io.tmpdir=/tmp/base/tmp -Djava.security.auth.login.config=/tmp/base/conf/login.config \
 -jar /opt/apache-activemq/bin/activemq.jar start xbean:/tmp/base/conf/activemq.xml
'''


def prepare(spec, output):
    fixtures = output / 'fixtures'
    (fixtures / 'activemq.xml').write_text(CONFIG)
    (fixtures / 'stomp-users.properties').write_text(USER+'='+PASSWORD+'\n')
    (fixtures / 'stomp-credentials.json').write_text(json.dumps({'user': USER, 'pass': PASSWORD}))
    spec['services']['activemq'] = {
        'image': IMAGE, 'entrypoint': ['/bin/sh', '-ec', START],
        'user': '65534:65534', 'read_only': True, 'cap_drop': ['ALL'],
        'security_opt': ['no-new-privileges:true'],
        'networks': list(spec['services']['runner']['networks']),
        'mem_limit': '512m', 'cpus': 2, 'pids_limit': 256,
        'stop_grace_period': '35s',
        'tmpfs': ['/tmp:rw,nosuid,nodev,size=256m,mode=1777'],
        'volumes': [{'type': 'bind', 'source': str(fixtures), 'target': '/fixtures', 'read_only': True}],
    }


def read_frame(reader):
    while True:
        command = reader.readline(4097)
        if len(command) > 4096 or not command.endswith(b'\n'):
            raise ValueError('owned STOMP command unavailable')
        command = command.rstrip(b'\r\n')
        if command:
            break
    headers, budget = {}, len(command)
    while True:
        line = reader.readline(4097)
        budget += len(line)
        if len(line) > 4096 or budget > 65536 or not line.endswith(b'\n'):
            raise ValueError('owned STOMP header invalid')
        line = line.rstrip(b'\r\n')
        if not line:
            break
        key, separator, value = line.partition(b':')
        if not separator:
            raise ValueError('owned STOMP header missing')
        headers.setdefault(key.decode(), value.decode())
    if 'content-length' in headers:
        length = int(headers['content-length'])
        if not 0 <= length <= 4 << 20:
            raise ValueError('owned STOMP body oversized')
        body = reader.read(length)
        if len(body) != length or reader.read(1) != b'\0':
            raise ValueError('owned STOMP body truncated')
    else:
        body = bytearray()
        while True:
            value = reader.read(1)
            if value == b'\0':
                break
            if not value or len(body) >= 4 << 20:
                raise ValueError('owned STOMP body invalid')
            body.extend(value)
        body = bytes(body)
    return command.decode(), headers, body


def write_frame(sock, command, headers, body=b''):
    values = [command]
    for key, value in headers.items():
        value = str(value)
        if any(c in key+value for c in '\0\r\n'):
            raise ValueError('owned STOMP framing invalid')
        values.append(key+':'+value)
    sock.sendall(('\n'.join(values)+'\n\n').encode()+body+b'\0')


@contextlib.contextmanager
def connection():
    with socket.create_connection(('activemq', 61613), timeout=3) as sock:
        reader = sock.makefile('rb')
        try:
            write_frame(sock, 'CONNECT', {'accept-version': '1.2', 'host': 'activemq',
                'login': USER, 'passcode': PASSWORD, 'heart-beat': '0,0'})
            command, headers, _ = read_frame(reader)
            assert command == 'CONNECTED' and headers['version'] == '1.2', 'owned broker handshake failed'
            yield sock, reader
        finally:
            reader.close()


def receipt(reader, expected):
    command, headers, _ = read_frame(reader)
    assert command == 'RECEIPT' and headers.get('receipt-id') == expected, 'owned broker receipt failed'


def send(body, queue=QUEUE):
    with connection() as (sock, reader):
        identifier = 'owned-'+uuid.uuid4().hex
        write_frame(sock, 'SEND', {'destination': queue, 'persistent': 'true',
            'content-type': 'application/json', 'content-length': len(body), 'receipt': identifier}, body)
        receipt(reader, identifier)


def queue_state(queue=QUEUE):
    name = queue.removeprefix('/queue/')
    assert name.replace('-', '').isalnum(), 'owned queue name invalid'
    request = {'type': 'read', 'mbean': 'org.apache.activemq:type=Broker,brokerName=wire-owned,destinationType=Queue,destinationName='+name,
        'attribute': ['QueueSize', 'InFlightCount', 'DispatchCount', 'DequeueCount', 'EnqueueCount']}
    return {key: int(value) for key, value in management(request).items()}


def management(request):
    client = http.client.HTTPConnection('activemq', 8161, timeout=3)
    try:
        client.request('POST', '/api/jolokia/', json.dumps(request), {
            'Content-Type': 'application/json', 'Origin': 'http://activemq:8161',
            'Authorization': 'Basic '+base64.b64encode((USER+':'+PASSWORD).encode()).decode()})
        response = client.getresponse()
        raw = response.read(1 << 20)
        assert response.status == 200, 'owned broker statistics HTTP failed'
        data = json.loads(raw)
        assert data['status'] == 200, 'owned broker statistics read failed'
        return data['value']
    finally:
        client.close()


def wait_ready():
    deadline = time.monotonic()+30
    while True:
        try:
            with connection():
                pass
            management({'type': 'version'})
            return
        except (OSError, ValueError, AssertionError):
            assert time.monotonic() < deadline, 'owned ActiveMQ readiness failed'
            time.sleep(0.2)


def failure_probe():
    deadline = time.monotonic()+20
    while True:
        baseline = queue_state()
        if baseline['QueueSize'] == baseline['InFlightCount'] == 0:
            break
        assert time.monotonic() < deadline, 'owned queue not quiescent before failure control'
        time.sleep(0.2)
    send(POISON)
    while True:
        state = queue_state()
        if state['DispatchCount'] >= baseline['DispatchCount']+2:
            assert state['DequeueCount'] == baseline['DequeueCount'] and state['QueueSize'] == 1, 'failed message was acknowledged'
            return {'baseline': baseline, 'redelivery': state}
        assert time.monotonic() < deadline, 'failed event was not redelivered'
        time.sleep(0.2)


def drain_failure():
    # Called only after the native/Go consumer has stopped and its listener closed.
    with connection() as (sock, reader):
        write_frame(sock, 'SUBSCRIBE', {'destination': QUEUE, 'id': 'owned-cleanup', 'ack': 'client-individual'})
        command, headers, body = read_frame(reader)
        assert command == 'MESSAGE' and body == POISON and headers.get('redelivered') == 'true', 'owned failure redelivery mismatch'
        identifier = 'cleanup-'+uuid.uuid4().hex
        write_frame(sock, 'ACK', {'id': headers['ack'], 'receipt': identifier})
        receipt(reader, identifier)
    state = queue_state()
    assert state['QueueSize'] == state['InFlightCount'] == 0, 'owned failure cleanup incomplete'
    return state
