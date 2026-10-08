import json, urllib.request, urllib.error
from pathlib import Path
port=json.loads(Path('/fixtures/peers.json').read_text())['0']['cannon']['cannon']['port']
user='11111111-1111-4111-8111-111111111111'
records=[]
for name,method,path,headers,body in [
 ('health','GET','/i/status',{},None),
 ('missing-principal','GET','/await',{},None),
 ('malformed-principal','GET','/await',{'Z-User':'invalid','Z-Connection':'fixture'},None),
 ('malformed-client','GET','/await?client=invalid',{'Z-User':user,'Z-Connection':'fixture'},None),
 ('malformed-push-user','POST','/i/push/invalid/fixture',{'Content-Type':'application/json'},b'{}'),
 ('malformed-bulk-json','POST','/i/bulkpush',{'Content-Type':'application/json'},b'{'),
 ('malformed-presence-user','HEAD','/i/presences/invalid/fixture',{},None),
 ('events-before-v8','GET','/v7/events',{'Z-User':user},None),
 ('events-malformed-user','GET','/v8/events',{'Z-User':'invalid'},None),
 ('non-upgrade-await','GET','/await',{'Z-User':user,'Z-Connection':'fixture'},None)]:
 headers['Request-Id']='cannon-reference-readonly'
 request=urllib.request.Request('http://127.0.0.1:'+str(port)+path,data=body,headers=headers,method=method)
 try: response=urllib.request.urlopen(request,timeout=5)
 except urllib.error.HTTPError as error: response=error
 with response:
  raw=response.read(8192)
  try: payload=json.loads(raw)
  except (json.JSONDecodeError,UnicodeDecodeError): payload=raw.decode('utf-8',errors='strict')
  records.append({'id':name,'method':method,'path':path,'headers':headers,'request_body':None if body is None else body.decode(),'status':response.status,'content_type':response.headers.get('Content-Type'),'allow':response.headers.get('Allow'),'response_body':payload})
print(json.dumps(records,indent=2))
