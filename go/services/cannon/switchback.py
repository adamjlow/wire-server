"""Exercise frozen queue/Cassandra switchback with real Haskell and Go binaries.

Uses only synthetic fixed IDs, the owned fixture utility's parameterized queries,
existing broker/schema and approved WebSocket frames. Does not edit the oracle.
"""
import importlib.util
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time

source=Path('/source/docs/go-migration/services/cannon/oracle/capture_boundaries.py')
spec=importlib.util.spec_from_file_location('protected_cannon_stimuli',source);stimuli=importlib.util.module_from_spec(spec);spec.loader.exec_module(stimuli)
stimuli.PORT=16183;stimuli.UID='33333333-3333-4333-8333-333333333333'
if os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise RuntimeError('private environment required')
reference,candidate,fixture=sys.argv[1:]
records=[];process=None;sockets=[]
with tempfile.TemporaryDirectory(prefix='cannon-switchback-') as directory:
    config=json.loads(Path('/fixtures/peers.json').read_text())['0']['cannon'];config['cannon'].update(port=16183,externalHost='127.0.0.1')
    config_path=Path(directory)/'config.json';config_path.write_text(json.dumps(config))
    def storage(mode,sequence=None):
        args=[fixture,'--config',str(config_path),'--mode',mode]
        if sequence is not None:args+=['--sequence',str(sequence)]
        done=subprocess.run(args,capture_output=True,text=True,check=True,timeout=15);return json.loads(done.stdout)
    def stop():
        global process
        if process is None:return
        started=time.monotonic();process.send_signal(signal.SIGTERM);code=process.wait(timeout=4)
        assert code==0;records.append({'name':'stop','implementation':implementation,'exit_code':code,'seconds':time.monotonic()-started})
        for socket in sockets:socket.close()
        sockets.clear();process=None
    def start(binary,label):
        global process,implementation
        implementation=label
        log=open(Path(directory)/(str(len(records))+'.log'),'w')
        process=subprocess.Popen([binary,'-c',str(config_path)],stdout=log,stderr=subprocess.STDOUT);log.close()
        def ready():
            if process.poll() is not None:raise RuntimeError('owned cannon exited during switchback')
            try:return stimuli.request('GET','/i/status')['status']==200
            except OSError:return False
        stimuli.until(ready)
        socket=stimuli.WS('/v9/events?client=abcd');sockets.append(socket);return socket
    def event(socket,sequence):
        frame=socket.recv();assert frame['opcode']==2;body=frame['payload'];assert body['type']=='event'
        assert body['data']['event']=={'id':'44444444-4444-4444-8444-444444444444','payload':[{'type':'synthetic-switchback','sequence':sequence}]}
        tag=body['data']['delivery_tag'];assert isinstance(tag,int) and tag>0
        records.append({'name':'event','implementation':implementation,'sequence':sequence,'delivery_tag':tag});return tag
    def ack(socket,tag):socket.send(1,json.dumps({'type':'ack','data':{'delivery_tag':tag,'multiple':False}}))
    def full_sync(socket):
        assert socket.recv()=={'opcode':2,'payload':{'type':'notifications_missed'}}
        records.append({'name':'full-sync','implementation':implementation})
    try:
        storage('delete-missed')
        socket=start(reference,'haskell');storage('publish',1);event(socket,1);stop()
        socket=start(candidate,'go');tag=event(socket,1);ack(socket,tag);storage('publish',2);event(socket,2);stop()
        socket=start(reference,'haskell');tag=event(socket,2);ack(socket,tag)
        # A fixture roundtrip plus observed subsequent switchback validates ack1
        # persisted; ack2 has a real broker/client opportunity before shutdown.
        assert storage('read-missed')['missed'] is False;stop()
        for writer,reader in ((candidate,reference),(reference,candidate)):
            storage('set-missed');assert storage('read-missed')['missed'] is True
            label='go' if writer==candidate else 'haskell';socket=start(writer,label);full_sync(socket);stop()
            assert storage('read-missed')['missed'] is True
            label='go' if reader==candidate else 'haskell';socket=start(reader,label);full_sync(socket)
            socket.send(1,'{"type":"ack_full_sync"}')
            stimuli.until(lambda:storage('read-missed')['missed'] is False)
            records.append({'name':'full-sync-delete','implementation':label,'missed':False});stop()
        storage('delete-queue');storage('delete-missed')
        print(json.dumps({'schema_version':1,'result':'pass','reference_sha256':hashlib.sha256(Path(reference).read_bytes()).hexdigest(),'candidate_sha256':hashlib.sha256(Path(candidate).read_bytes()).hexdigest(),'fixture_sha256':hashlib.sha256(Path(fixture).read_bytes()).hexdigest(),'records':records,'cleanup':'owned processes reaped; synthetic client queue and missed row deleted'},indent=2))
    finally:
        for socket in sockets:socket.close()
        if process is not None and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=4)
            except subprocess.TimeoutExpired:process.kill();process.wait()

        pending_error = sys.exc_info()[0] is not None
        # Always remove our synthetic persistent resources, including on failure.
        for mode in ('delete-queue', 'delete-missed'):
            try: storage(mode)
            except (subprocess.SubprocessError, OSError):
                if not pending_error: raise
