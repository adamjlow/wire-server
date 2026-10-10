"""Finite synthetic probes for the pinned native multipart/urlencoded parser."""
import re


LIMIT=65336
BOUNDARY='spar-synthetic-boundary'


def multipart(fields,files=0,extra_headers=()):
    body=bytearray()
    for name,value in fields:
        body.extend(('--'+BOUNDARY+'\r\nContent-Disposition: form-data; name="'+name+'"\r\n').encode())
        for header in extra_headers:body.extend(header.encode()+b'\r\n')
        body.extend(b'\r\n'+value+b'\r\n')
    for index in range(files):
        body.extend(('--'+BOUNDARY+'\r\nContent-Disposition: form-data; name="file"; filename="synthetic.txt"\r\n\r\nsynthetic\r\n').encode())
    body.extend(('--'+BOUNDARY+'--\r\n').encode())
    return bytes(body)


def cases():
    urltype='application/x-www-form-urlencoded'
    multitype='multipart/form-data; boundary='+BOUNDARY
    result=[]
    for size in (LIMIT-1,LIMIT,LIMIT+1):
        prefix=b'SAMLResponse=dG9rZW4=&filler='
        result.append(('url-size-'+str(size),prefix+b'x'*(size-len(prefix)),urltype))
        fields=[('SAMLResponse',b'dG9rZW4='),('filler',b'x'*(size-len('SAMLResponse')-8-len('filler')))]
        result.append(('multipart-size-'+str(size),multipart(fields),multitype))
    result.append(('url-encoded-raw-size',b'SAMLResponse=dG9rZW4=&filler='+b'%61'*22000,urltype))
    for size in (32,33):result.append(('multipart-name-'+str(size),multipart([('x'*size,b'v'),('SAMLResponse',b'dG9rZW4=')]),multitype))
    for count in (10,11):result.append(('multipart-files-'+str(count),multipart([('SAMLResponse',b'dG9rZW4=')],files=count),multitype))
    for count in (32,33):result.append(('multipart-headers-'+str(count),multipart([('SAMLResponse',b'dG9rZW4=')],extra_headers=['X-Synthetic-'+str(n)+': x' for n in range(count-1)]),multitype))
    # Native line length includes CR, excludes LF.
    for size in (8190,8191):result.append(('multipart-header-bytes-'+str(size),multipart([('SAMLResponse',b'dG9rZW4=')],extra_headers=['X-Pad: '+'x'*(size-8)]),multitype))
    result.extend([
        ('url-invalid-utf8',b'SAMLResponse=%FF',urltype),
        ('url-semicolon',b'filler=a;b&SAMLResponse=dG9rZW4=',urltype),
        ('url-duplicate',b'SAMLResponse=dG9rZW4=&SAMLResponse=other',urltype),
        ('unsupported-json',b'{}','application/json'),
        ('missing-multipart-boundary',b'','multipart/form-data'),
    ])
    assert len(result)==20
    return result


def projection(reply):
    body=reply['body']
    if isinstance(body,str):
        title=re.search(r'<title>([^<]+)</title>',body)
        if title:body={'html_title':title.group(1)}
    return {**reply,'body':body}
