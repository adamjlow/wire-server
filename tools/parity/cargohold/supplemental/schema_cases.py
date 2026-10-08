"""Three concrete metadata validation gaps found during the final source audit."""
import json
from cases import OWNER, DATA


def run(ref,ports,start,stop,request,store_effects,hs,go):
    records=[]
    for label,settings in [('empty-retention',{'public':False,'retention':''}),
                            ('invalid-conversation-uuid',{'public':False,'convId':{'id':'invalid','domain':'example.com'}}),
                            ('invalid-audit-mime',{'public':False,'filetype':'invalid'})]:
        metadata=json.dumps(settings,separators=(',',':')).encode()
        body=b'--wire\r\nContent-Type: application/json\r\nContent-Length: '+str(len(metadata)).encode()+b'\r\n\r\n'+metadata+b'\r\n--wire\r\nContent-Type: text/plain\r\nContent-Length: 13\r\n\r\n'+DATA+b'\r\n--wire--\r\n'
        results={}
        for kind in ports:
            before={o['Key'] for o in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
            response=request(kind,'POST','/bot/assets',{'Z-Bot':OWNER,'Z-Type':'bot','Content-Type':'multipart/mixed; boundary=wire'},body)
            from extended import normalize_upload
            normalize_upload(response)
            results[kind]={'response':response,'objects':store_effects(before),'open_uploads':ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])}
        records.append({'id':label,'responses':results,'equal':results['haskell']==results['go']})
    return records
