"""One real native-Go-native Spar handover on unchanged owned state."""
import base64
import hashlib
from html.parser import HTMLParser
import http.client as http_client
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import urllib.parse
import uuid

sys.path.insert(0,'/opt/wire-e2e')
import runtime

REPORT=Path('/reports/switchback-progress.json')
output={'result':'running','kind':'durable-native-Go-native-Spar','e2e_executed':False,'owners':[],'actions':[],'snapshots':{}}
PORTS={}


def save():REPORT.write_text(json.dumps(output,indent=2)+'\n')


def http(service,method,path,body=None,headers=None,expected=None):
    headers={'Request-Id':'spar-switchback-synthetic',**(headers or {})}
    if isinstance(body,(dict,list)):
        body=json.dumps(body);headers.setdefault('Content-Type','application/json')
    connection=http_client.HTTPConnection('127.0.0.1',PORTS[service],timeout=15)
    try:
        connection.request(method,path,body,headers)
        response=connection.getresponse();data=response.read(1024*1024+1)
        if len(data)>1024*1024:raise ValueError('oversized switchback response')
        text=data.decode();reply={'status':response.status,'headers':dict(response.getheaders()),'body':text}
        try:reply['body']=json.loads(text)
        except json.JSONDecodeError:pass
    finally:connection.close()
    recorded=json.loads(json.dumps(reply))
    for key,value in list(recorded['headers'].items()):
        if key.lower()=='set-cookie':recorded['headers'][key]='synthetic-cookie-sha256:'+hashlib.sha256(value.encode()).hexdigest()
        if key.lower()=='location' and value.startswith('wire://granted/'):
            recorded['headers'][key]='wire://granted/[synthetic-cookie]/'+value.rsplit('/',1)[-1]
    if isinstance(recorded['body'],dict) and 'token' in recorded['body']:
        recorded['body']['token']='synthetic-token-sha256:'+hashlib.sha256(recorded['body']['token'].encode()).hexdigest()
    output['actions'].append({'service':service,'method':method,'path':path,'reply':recorded});save()
    if expected is not None and reply['status'] not in ([expected] if isinstance(expected,int) else expected):raise ValueError(service+' '+method+' '+path+' unexpected status '+str(reply['status']))
    return reply


def header(reply,name):return next((value for key,value in reply['headers'].items() if key.lower()==name.lower()),None)


class RequestForm(HTMLParser):
    def __init__(self):super().__init__();self.request=None
    def handle_starttag(self,tag,attributes):
        values=dict(attributes)
        if tag=='input' and values.get('name')=='SAMLRequest':self.request=values.get('value')


def pending(idp,team,email,mobile=False,assertion=None):
    query={'label':'synthetic-switchback-label'}
    if mobile:query.update(success_redirect='wire://granted/$cookie/$userid',error_redirect='wire://denied/$label')
    reply=http('spar','GET','/sso/initiate-login/'+idp+'?'+urllib.parse.urlencode(query),expected=200)
    form=RequestForm();form.feed(reply['body'])
    if form.request is None:raise ValueError('actual SP AuthnRequest absent')
    metadata=http('spar','GET','/sso/metadata/'+team,expected=200)['body']
    signed=http('fixture','POST','/sign',{'request':form.request,'metadata':metadata,'subject':email,'assertionID':assertion or ''},expected=200)['body']
    signed['mobile']=mobile
    return signed


def finish(team,user,signed,accepted=True):
    data=urllib.parse.urlencode({'SAMLResponse':base64.b64encode(signed['xml'].encode()).decode()})
    reply=http('spar','POST','/sso/finalize-login/'+team,data,{'Content-Type':'application/x-www-form-urlencoded'},expected=303 if accepted and signed['mobile'] else 200)
    cookie=header(reply,'Set-Cookie')
    if accepted:
        if cookie is None or ('u='+user) not in cookie:raise ValueError('verified callback did not create the expected account session')
        if signed['mobile']:
            location=header(reply,'Location')
            if location is None or not location.startswith('wire://granted/') or not location.endswith('/'+user) or '$cookie' in location:raise ValueError('mobile verdict state/substitution failed')
        elif '<title>wire:sso:success</title>' not in reply['body']:raise ValueError('web verdict failed')
    elif cookie is not None or '<title>wire:sso:error:forbidden</title>' not in reply['body']:raise ValueError('replayed assertion created a session or missed denial boundary')


def bind(team,idp,user,email,issuer):return http('fixture','POST','/state',{'action':'bind','team':team,'idp':idp,'user':user,'external':email,'issuer':issuer},expected=200)['body']


def snapshot(label,requests,assertions):
    value=http('fixture','POST','/state',{'action':'snapshot','requests':requests,'assertions':assertions},expected=200)['body']
    output['snapshots'][label]=value;save();return value


def assert_pending(state,signed):
    request=state['authreq'][signed['request_id']];verdict=state['verdict'][signed['request_id']]
    if len(request)!=1 or len(verdict)!=1 or request[0]['ttl']<=0 or verdict[0]['ttl']<=0 or verdict[0]['format_con']!=(1 if signed['mobile'] else 0) or verdict[0]['cookie_label']!='synthetic-switchback-label':raise ValueError('real pending request/verdict state incomplete')


def surface(owner,token,idp,user,group):
    auth={'Z-User':owner}
    scim={'Authorization':'Bearer '+token,'Accept':'application/scim+json'}
    result={name:http('spar','GET',path,headers=headers,expected=200)['body'] for name,path,headers in [('idp','/identity-providers/'+idp,auth),('tokens','/scim/auth-tokens',auth),('user','/scim/v2/Users/'+user,scim),('group','/scim/v2/Groups/'+group,scim)]}
    result['brig_account']=http('brig','GET','/i/users?ids='+user+'&includePendingInvitations=true',expected=200)['body']
    return result


def main():
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot disposable environment required')
    processes=[];logs=[];servers=[];helper=None;owner_process=None
    try:
        servers=runtime.start_forwards();runtime.wait_dependencies();runtime.stage();runtime.initialize()
        peers=json.loads(Path('/fixtures/peers.json').read_text())['0']
        PORTS.update({name:peers[name][name]['port'] for name in ('brig','galley','spar')});PORTS['fixture']=8123
        output['candidate_sha256']=hashlib.sha256(Path('/candidate/spar').read_bytes()).hexdigest()
        runtime.start_peers(processes,logs);os.chdir(runtime.WORK)
        native=(runtime.WORK/'dist/spar').resolve(strict=True)
        owner_process=next(p for p in processes if Path(p.args[0]).name=='spar' and str(p.args[2]).endswith('spar-static-0.json'))
        config=runtime.WORK/'spar-static-0.json'
        output['config_sha256']=hashlib.sha256(config.read_bytes()).hexdigest()
        output['native_sha256']=hashlib.sha256(native.read_bytes()).hexdigest()
        output['owners'].append({'implementation':'haskell','phase':'native-first','pid':owner_process.pid,'sha256':output['native_sha256']})
        log=open('/reports/switchback-fixture.log','w');logs.append(log)
        helper=subprocess.Popen(['/fixtures/switchback-fixtures.test','-test.run=^TestNativeDigestAndLargeAssertionCompatibility$','-test.timeout=900s'],stdout=log,stderr=subprocess.STDOUT,env={**os.environ,'SPAR_SWITCHBACK_FIXTURE_CONFIG':str(config)})
        processes.append(helper)
        deadline=time.monotonic()+45
        while True:
            if helper.poll()is not None:raise ValueError('synthetic state/signing fixture exited')
            try:metadata=http('fixture','GET','/metadata',expected=200)['body'];break
            except (OSError,http_client.HTTPException):pass
            if time.monotonic()>deadline:raise ValueError('state fixture readiness timeout')
            time.sleep(.1)
        issuer=re.search(r'entityID="([^"]+)"',metadata).group(1)
        password='synthetic-switchback-password-2026'
        def team():
            account=http('brig','POST','/i/users',{'name':'Synthetic Switchback Owner','email':'synthetic-owner-'+uuid.uuid4().hex+'@example.test','password':password,'icon':'default','team':{'name':'Synthetic Switchback','icon':'default'}},expected=201)['body']
            for feature,status in [('sso','enabled'),('validateSAMLemails','disabled')]:http('galley','PATCH','/i/teams/'+account['team']+'/features/'+feature,{'status':status},expected=200)
            return account['id'],account['team']
        def token(caller,idp=None):return http('spar','POST','/scim/auth-tokens',{'password':password,'description':'synthetic switchback','name':'synthetic switchback','idp':idp},{'Z-User':caller},expected=200)['body']
        def user_payload(email):return {'schemas':['urn:ietf:params:scim:schemas:core:2.0:User','urn:wire:scim:schemas:profile:1.0','urn:ietf:params:scim:schemas:extension:wire:1.0:User'],'userName':'synthetic'+uuid.uuid4().hex[:12],'externalId':email,'displayName':'Synthetic Switchback User','active':True,'preferredLanguage':'en','roles':[{'value':'member'}],'emails':[{'value':email,'type':'work','primary':True}],'urn:ietf:params:scim:schemas:extension:wire:1.0:User':{'department':'synthetic'}}
        owner,tid=team()
        idp=http('spar','POST','/identity-providers?api_version=v2',metadata,{'Content-Type':'application/xml','Z-User':owner},expected=201)['body']['id']
        minted=token(owner,idp);bearer=minted['token']
        email='synthetic-user-'+uuid.uuid4().hex+'@example.test'
        user=http('spar','POST','/scim/v2/Users',user_payload(email),{'Authorization':'Bearer '+bearer},expected=201)['body'];uid=user['id']
        group=http('spar','POST','/scim/v2/Groups',{'schemas':['urn:ietf:params:scim:schemas:core:2.0:Group'],'displayName':'Synthetic Switchback Group','members':[{'value':uid,'type':'User','$ref':user['meta']['location']}]},{'Authorization':'Bearer '+bearer},expected=201)['body'];gid=group['id']
        bind(tid,idp,uid,email,issuer)
        legacy='synthetic-switchback-'+uuid.uuid4().hex;legacy_id=str(uuid.uuid4())
        http('fixture','POST','/state',{'action':'legacy-token','token':legacy,'tokenID':legacy_id},expected=200)
        initial=surface(owner,bearer,idp,uid,gid)
        completed=pending(idp,tid,email);finish(tid,uid,completed)
        native_web=pending(idp,tid,email);native_mobile=pending(idp,tid,email,True)
        requests=[v['request_id'] for v in (completed,native_web,native_mobile)];assertions=[v['assertion_id'] for v in (completed,native_web,native_mobile)]
        before=snapshot('native-before-handover',requests,assertions)
        for value in (native_web,native_mobile):assert_pending(before,value)
        if len(before['authresp'][completed['assertion_id']])!=1 or len(before['legacy_token_lookup'])!=1 or before['legacy_token_lookup'][0]['token_']!=legacy:raise ValueError('native replay or historical token prerequisite absent')
        def stop():
            nonlocal owner_process
            started=time.monotonic();owner_process.send_signal(signal.SIGTERM);code=owner_process.wait(timeout=35)
            output['owners'][-1]['shutdown']={'exit_code':code,'seconds':time.monotonic()-started};save()
            if code!=0:raise ValueError('Spar owner failed clean shutdown')
            owner_process=None
        def start(kind):
            nonlocal owner_process
            if owner_process is not None:raise ValueError('simultaneous Spar owners forbidden')
            binary='/candidate/spar' if kind=='go' else str(native)
            log=open('/reports/spar-switchback-'+kind+'.log','w');logs.append(log)
            owner_process=subprocess.Popen([binary,'-c',str(config)],cwd=runtime.WORK/'services/spar',stdout=log,stderr=subprocess.STDOUT);processes.append(owner_process)
            output['owners'].append({'implementation':kind,'phase':'go-middle' if kind=='go' else 'native-return','pid':owner_process.pid,'sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest()});save()
            deadline=time.monotonic()+45
            while True:
                if owner_process.poll()is not None:raise ValueError('new Spar owner exited')
                try:
                    if http('spar','GET','/i/status')['status']==200:return
                except (OSError,http_client.HTTPException):pass
                if time.monotonic()>deadline:raise ValueError('Spar handover readiness timeout')
                time.sleep(.1)
        stop();start('go')
        if surface(owner,bearer,idp,uid,gid)!=initial:raise ValueError('Go cannot read identical native IdP/token/user/group metadata')
        if http('spar','GET','/scim/v2/Users/'+uid,headers={'Authorization':'Bearer '+legacy},expected=200)['body']!=initial['user']:raise ValueError('legacy token resource changed')
        converted=snapshot('go-converted-legacy',requests,assertions)
        if len(converted['legacy_token_lookup'])!=1 or not converted['legacy_token_lookup'][0]['token_'].startswith('sha512:') or converted['legacy_token_lookup'][0]['id']!=legacy_id:raise ValueError('Go legacy token conversion failed')
        for value in (native_web,native_mobile):finish(tid,uid,value)
        replay=pending(idp,tid,email,assertion=completed['assertion_id']);finish(tid,uid,replay,False)
        updated={**initial['user'],'displayName':'Synthetic Go Updated User'}
        http('spar','PUT','/scim/v2/Users/'+uid,updated,{'Authorization':'Bearer '+bearer},expected=200)
        updated_group={**initial['group'],'displayName':'Synthetic Go Updated Group'}
        http('spar','PUT','/scim/v2/Groups/'+gid,updated_group,{'Authorization':'Bearer '+bearer},expected=200)
        go_surface=surface(owner,bearer,idp,uid,gid)
        go_web=pending(idp,tid,email);go_mobile=pending(idp,tid,email,True)
        requests += [v['request_id'] for v in (replay,go_web,go_mobile)];assertions += [v['assertion_id'] for v in (go_web,go_mobile)]
        go_before=snapshot('go-before-return',requests,assertions)
        for value in (go_web,go_mobile):assert_pending(go_before,value)
        if go_before['authreq'][native_web['request_id']] or go_before['authreq'][native_mobile['request_id']] or len(go_before['authresp'][native_web['assertion_id']])!=1:raise ValueError('Go did not consume native requests/store assertion replay')
        stop();start('haskell')
        if surface(owner,bearer,idp,uid,gid)!=go_surface:raise ValueError('returning native cannot read identical Go metadata/updates')
        if http('spar','GET','/scim/v2/Users/'+uid,headers={'Authorization':'Bearer '+legacy},expected=200)['body']!=go_surface['user']:raise ValueError('returning native cannot use converted legacy token')
        for value in (go_web,go_mobile):finish(tid,uid,value)
        replay_return=pending(idp,tid,email,assertion=native_web['assertion_id']);finish(tid,uid,replay_return,False)
        requests.append(replay_return['request_id'])
        returned=snapshot('native-after-return',requests,assertions)
        if returned['authreq'][go_web['request_id']] or returned['authreq'][go_mobile['request_id']] or len(returned['authresp'][go_web['assertion_id']])!=1:raise ValueError('returning native lost request/assertion state')
        http('spar','DELETE','/scim/v2/Groups/'+gid,headers={'Authorization':'Bearer '+bearer},expected=204)
        http('spar','GET','/scim/v2/Groups/'+gid,headers={'Authorization':'Bearer '+bearer},expected=404)
        http('spar','DELETE','/scim/v2/Users/'+uid,headers={'Authorization':'Bearer '+bearer},expected=204)
        http('spar','GET','/scim/v2/Users/'+uid,headers={'Authorization':'Bearer '+bearer},expected=404)
        for token_id in (minted['info']['id'],legacy_id):http('spar','DELETE','/scim/auth-tokens?id='+token_id,headers={'Z-User':owner},expected=204)
        http('spar','DELETE','/identity-providers/'+idp,headers={'Z-User':owner},expected=204)
        deleted=snapshot('native-deleted-Go-state',requests,assertions)
        if any(deleted[name] for name in ('idp','team_idp','issuer_idp_v2','tokens','legacy_token_lookup','user_v2','scim_external','scim_meta')):raise ValueError('returning native deletion left incompatible service state')
        stop();helper.send_signal(signal.SIGTERM)
        if helper.wait(timeout=15)!=0:raise ValueError('fixture process failed')
        output.update(result='pass',original_e2e_executed=False,scope='Actual same-state API/CQL native-Go-native IdP/token/legacy conversion/SCIM user/group/metadata/pending web-mobile requests/verdict/replay and returning-native deletion. Not final225 or operations/image/selector qualification.');return 0
    except Exception as error:
        output.update(result='fail',error_category=type(error).__name__,error=str(error)[:300]);return 1
    finally:
        for process in processes:
            if process.poll()is None:process.terminate()
        for process in processes:
            try:process.wait(timeout=5)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        for log in logs:log.close()
        for server in servers:server.server_close()
        save();Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')


if __name__=='__main__':raise SystemExit(main())
