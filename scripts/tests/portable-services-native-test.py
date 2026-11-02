"""Опциональный native smoke: pinned images, rootless Docker и synthetic credentials.

Зависимости: portable-services-native-requirements.txt. Production endpoints/credentials не принимаются.
"""
import signal
import pathlib, tempfile, os, subprocess, json, importlib.util, yaml, secrets, bcrypt, time, requests, re, hashlib
root=pathlib.Path(__file__).resolve().parents[2]
state=pathlib.Path(tempfile.mkdtemp(prefix='kodex-portable-native-'));state.chmod(0o700)
endpoint=subprocess.check_output(['docker','context','inspect','rootless','--format','{{.Endpoints.docker.Host}}'],text=True).strip()
if not endpoint.startswith('unix:///run/user/')or not endpoint.endswith('/docker.sock'):raise SystemExit('A local rootless Docker endpoint is required')
d=pathlib.Path(tempfile.mkdtemp(prefix='portable-native-',dir=state));d.chmod(0o755)
env={'PATH':os.environ['PATH'],'HOME':str(d),'DOCKER_CONFIG':str(d/'docker'),'DOCKER_HOST':endpoint};(d/'docker').mkdir(mode=0o700)
spec=importlib.util.spec_from_file_location('contract',root/'scripts/tests/portable-services-test.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
v=m.profile();objects=list(filter(None,yaml.safe_load_all(m.render(v).stdout)))
for part in ['console','registry']:
 obj=next(x['data']for x in objects if x['kind']=='ConfigMap'and x['metadata']['name']=='synthetic-'+part)
 folder=d/part;folder.mkdir(mode=0o755)
 for name,value in obj.items():(folder/name).write_text(value)
(d/'tls').mkdir();subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=synthetic.example.invalid','-addext','subjectAltName=IP:127.0.0.1,DNS:synthetic-registry.synthetic-infra.svc.cluster.local,DNS:synthetic-storage.synthetic-infra.svc.cluster.local','-keyout',str(d/'tls/tls.key'),'-out',str(d/'tls/tls.crt')],capture_output=True,check=True)
(d/'tls/ca.crt').write_bytes((d/'tls/tls.crt').read_bytes());(d/'tls/tls.key').chmod(0o644)
for name in ['prepared','data','registry-data']:(d/name).mkdir(mode=0o777);(d/name).chmod(0o777)
for name in ['master','volume','admin']:(d/'data'/name).mkdir(mode=0o777);(d/'data'/name).chmod(0o777)
credential={'accessKey':secrets.token_hex(16),'secretKey':secrets.token_hex(32)};pw=secrets.token_urlsafe(36);writer=secrets.token_urlsafe(36);reader=secrets.token_urlsafe(36)
prepared=d/'prepared';(prepared/'s3.json').write_text(m.PREPARE.storage_config(json.dumps({'identities':[{'name':'synthetic-owner','credentials':[credential],'actions':['Admin','Read','Write','List','Tagging']}]})))
(prepared/'security.toml').write_text(m.PREPARE.admin_config('synthetic-owner',pw));(prepared/'htpasswd').write_text(''.join(u+':'+bcrypt.hashpw(p.encode(),bcrypt.gensalt(10)).decode()+'\n'for u,p in [('release-writer',writer),('release-reader',reader)]))
for p in prepared.iterdir():p.chmod(0o644)
# Только synthetic credentials для собственных непривилегированных тестовых контейнеров.
def terminate(signum, frame):
 raise KeyboardInterrupt
signal.signal(signal.SIGTERM,terminate)
names=[];receipt={'directory':str(d),'checks':[]};session=requests.Session();session.trust_env=False
base=['docker','run','--cpus','2','--memory','2g','--detach','--read-only','--user','10000:10000','--cap-drop=ALL','--security-opt','no-new-privileges','--tmpfs','/tmp:rw,nosuid,nodev,mode=1777']
def docker(args,timeout=40):
 r=subprocess.run(['docker',*args],env=env,capture_output=True,text=True,timeout=timeout)
 if r.returncode:raise RuntimeError('native docker operation failed')
 return r.stdout.strip()
def mount(src,dst):return ['--mount',f'type=bind,src={src},dst={dst}']
def start(name,image,args,network=None,ports=None,folder=None):
 name='kodex-native-'+name+'-'+d.name[-8:];cmd=base+['--name',name]
 cmd+=['--network',network]if network else []
 for port in ports or []:cmd+=['-p',f'127.0.0.1:{port}:{port}']
 cmd+=mount(d/'tls','/tls')+mount(prepared,'/prepared')
 if folder:cmd+=mount(d/folder,'/configuration')
 cmd+=['--entrypoint',args[0],image,*args[1:]]
 r=subprocess.run(cmd,env=env,capture_output=True,text=True,timeout=45)
 if r.returncode:raise RuntimeError('native container launch failed')
 names.append(name);return name
try:
 for part in ['console','registry']:
  r=subprocess.run(['docker','run','--rm','--read-only','--user','10000:10000','--network','none','--cap-drop=ALL','--tmpfs','/tmp:rw,nosuid,nodev,mode=1777']+mount(d/'tls','/tls')+mount(prepared,'/prepared')+mount(d/part,'/configuration')+['--entrypoint','/usr/sbin/nginx',v['images']['nginx'],'-t','-c','/configuration/nginx.conf'],env=env,capture_output=True,text=True,timeout=40)
  receipt['checks'].append({'name':part+'-nginx-uid10000-config','pass':r.returncode==0,'diagnostic':r.stderr[-1200:]});assert r.returncode==0
 # Хранилище запускается с data mount и точной командой, полученной из chart.
 anchor=start('pod-network',v['images']['nginx'],['/bin/sh','-c','exec sleep 600'],ports=[8333,23647])
 sts=next(x for x in objects if x['kind']=='StatefulSet'and x['metadata']['name']=='synthetic-storage');containers=sts['spec']['template']['spec']['containers'];storage=next(x for x in containers if x['name']=='storage')
 name='kodex-native-storage-'+d.name[-8:];cmd=base+['--name',name,'--workdir','/data','--network','container:'+anchor]+mount(d/'tls','/tls')+mount(prepared,'/prepared')+mount(d/'data','/data')+['--entrypoint','/usr/bin/weed',v['images']['storage'],*storage['args']]
 r=subprocess.run(cmd,env=env,capture_output=True,timeout=45);assert r.returncode==0;names.append(name)
 console=next(x for x in containers if x['name']=='console');n='kodex-native-ui-'+d.name[-8:]
 r=subprocess.run(base+['--name',n,'--network','container:'+anchor,'--workdir','/data/admin']+mount(d/'tls','/tls')+mount(prepared,'/prepared')+mount(d/'data','/data')+['--entrypoint','/usr/bin/weed',v['images']['storage'],*console['args']],env=env,capture_output=True,timeout=45);assert r.returncode==0;names.append(n)
 start('console-gateway',v['images']['nginx'],['/usr/sbin/nginx','-g','daemon off;','-c','/configuration/nginx.conf'],network='container:'+anchor,folder='console')
 url='https://127.0.0.1:23647/storage/login'
 for attempt in range(25):
  try:
   r=session.get(url,headers={'Host':'app.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=3,allow_redirects=False)
   if r.status_code==200:break
  except requests.RequestException:pass
  time.sleep(1)
 else:raise RuntimeError('native UI unavailable')
 receipt['checks'].append({'name':'UI-prefixed-login-page','pass':r.status_code==200,'status':r.status_code})
 (d/'ui-login.html').write_text(r.text)
 # Фиксируются имена полей synthetic страницы, без значений cookie.
 receipt['form_fields']=re.findall(r'<input[^>]+name="([^"]+)"',r.text)

 def check(label,condition,**details):
  receipt['checks'].append({'name':label,'pass':bool(condition),**details})
  if not condition:raise RuntimeError('native contract failed: '+label)
 token=re.search(r'name="csrf_token"[^>]*value="([^"]+)"',r.text)
 if not token:token=re.search(r'value="([^"]+)"[^>]*name="csrf_token"',r.text)
 assert token
 origin='https://app.example.invalid'
 r=session.post(url,data={'csrf_token':token.group(1),'username':'synthetic-owner','password':pw},headers={'Origin':origin,'Host':'app.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=10,allow_redirects=False)
 check('UI-authenticated-login',r.status_code in [302,303]and r.headers.get('Location','')=='/storage/admin',status=r.status_code,location=r.headers.get('Location',''))
 cookie=r.headers.get('Set-Cookie','')
 check('UI-cookie-security',all(x.lower()in cookie.lower()for x in ['secure','httponly','samesite=strict','path=/storage']),attributes=[part.strip()for part in cookie.split(';')[1:]])
 r=session.get('https://127.0.0.1:23647/storage/admin',headers={'Host':'app.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=10,allow_redirects=False)
 check('UI-authenticated-prefix',r.status_code==200,status=r.status_code)
 assets=re.findall(r'(?:href|src)="(/storage/[^"?#]+\.(?:js|css))',r.text)
 check('UI-assets-use-prefix',bool(assets),count=len(assets))
 for asset in sorted(set(assets))[:3]:
  response=session.get('https://127.0.0.1:23647'+asset,headers={'Host':'app.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=10)
  check('UI-asset',response.status_code==200,status=response.status_code)
 response=session.post('https://127.0.0.1:23647/storage/logout',headers={'Origin':'https://other.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=5,allow_redirects=False)
 check('UI-cross-origin-write-denied',response.status_code==403,status=response.status_code)
 # Logout проверяется по собственному контракту страницы.
 (d/'ui-authenticated.html').write_text(r.text)
 receipt['logout_links']=re.findall(r'<(?:a|form)[^>]*(?:href|action)="([^"]*logout[^"]*)"[^>]*>',r.text)
 token2=re.search(r'name="csrf_token"[^>]*value="([^"]+)"',r.text)
 if not token2:token2=re.search(r'<meta[^>]*name="csrf-token"[^>]*content="([^"]+)"',r.text)
 receipt['logout_token_available']=bool(token2)
 response=session.get('https://127.0.0.1:23647/storage/logout',headers={'Host':'app.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=10,allow_redirects=False)
 check('UI-logout-prefixed-redirect',response.status_code==303 and response.headers.get('Location')=='/storage/login',status=response.status_code,attributes=[part.strip()for part in response.headers.get('Set-Cookie','').split(';')[1:]],cookie_paths=[{'name':c.name,'domain':c.domain,'path':c.path}for c in session.cookies])
 response=session.get('https://127.0.0.1:23647/storage/admin',headers={'Host':'app.example.invalid'},verify=str(d/'tls/ca.crt'),timeout=10,allow_redirects=False)
 check('UI-logout-closes-session',response.status_code in [302,303,307] and response.headers.get('Location','').startswith('/storage/login'),status=response.status_code,location=response.headers.get('Location',''))

 regname='kodex-native-registry-'+d.name[-8:]
 cmd=base+['--name',regname,'-p','127.0.0.1:8443:8443']+mount(d/'tls','/tls')+mount(d/'registry','/configuration')+mount(d/'registry-data','/data')+['--entrypoint','/bin/registry',v['images']['registry'],'serve','/configuration/registry.json']
 result=subprocess.run(cmd,env=env,capture_output=True,timeout=45);assert result.returncode==0;names.append(regname)
 start('registry-gateway',v['images']['nginx'],['/usr/sbin/nginx','-g','daemon off;','-c','/configuration/nginx.conf'],network='container:'+regname,folder='registry')
 ru='https://127.0.0.1:8443'
 for attempt in range(20):
  try:
   response=session.get(ru+'/v2/',verify=str(d/'tls/ca.crt'),timeout=3)
   if response.status_code==401:break
  except requests.RequestException:pass
  time.sleep(1)
 check('OCI-anonymous-challenge',response.status_code==401 and 'Basic'in response.headers.get('WWW-Authenticate',''),status=response.status_code)
 def req(method,path,auth=None,**kwargs):return session.request(method,ru+path,auth=auth,verify=str(d/'tls/ca.crt'),timeout=10,allow_redirects=False,**kwargs)
 response=req('GET','/v2/',('release-reader',reader));check('OCI-reader-read',response.status_code==200,status=response.status_code)
 response=req('POST','/v2/kodex/synthetic/blobs/uploads/',('release-reader',reader));check('OCI-reader-write-denied',response.status_code==403,status=response.status_code)
 response=req('GET','/v2/',('release-writer','invalid'));check('OCI-invalid-key-denied',response.status_code==401,status=response.status_code)
 response=req('POST','/v2/other/synthetic/blobs/uploads/',('release-writer',writer));check('OCI-unlisted-repository-denied',response.status_code==404,status=response.status_code)
 response=req('POST','/v2/kodex/synthetic/blobs/uploads/',('release-writer',writer));check('OCI-writer-create',response.status_code==202,status=response.status_code)
 location=response.headers['Location'];check('OCI-upload-location-relative',location.startswith('/v2/kodex/synthetic/blobs/uploads/'))
 blob=b'{}';digest='sha256:'+hashlib.sha256(blob).hexdigest();separator='&'if '?'in location else '?'
 response=req('PUT',location+separator+'digest='+digest,('release-writer',writer),data=blob,headers={'Content-Type':'application/octet-stream'});check('OCI-writer-upload',response.status_code==201,status=response.status_code)
 response=req('GET','/v2/kodex/synthetic/blobs/'+digest,('release-reader',reader));check('OCI-readback-by-digest',response.status_code==200 and response.content==blob,status=response.status_code)
 # Отдельный TLS proxy проверяет настоящий pinned S3 verifier подписанными SDK-запросами.
 gateway=d/'s3-proxy';gateway.mkdir()
 ip=next(iter(json.loads(docker(['inspect',anchor]))[0]['NetworkSettings']['Networks'].values()))['IPAddress']
 (gateway/'static.yaml').write_text(yaml.safe_dump({'entryPoints':{'websecure':{'address':':9443'}},'providers':{'file':{'filename':'/configuration/dynamic.yaml'}},'log':{'level':'ERROR'}}))
 dynamic={'tls':{'stores':{'default':{'defaultCertificate':{'certFile':'/tls/tls.crt','keyFile':'/tls/tls.key'}}},'certificates':[{'certFile':'/tls/tls.crt','keyFile':'/tls/tls.key'}],'options':{'default':{'minVersion':'VersionTLS13'}}},'http':{'routers':{'s3':{'entryPoints':['websecure'],'rule':'PathPrefix(`/s3/`)','service':'s3','middlewares':['strip','origin'],'tls':{}}},'middlewares':{'strip':{'stripPrefix':{'prefixes':['/s3']}},'origin':{'headers':{'customRequestHeaders':{'X-Forwarded-Prefix':'/s3','Cookie':''}}}},'services':{'s3':{'loadBalancer':{'servers':[{'url':'https://'+ip+':8333'}],'serversTransport':'storage'}}},'serversTransports':{'storage':{'serverName':'synthetic-storage.synthetic-infra.svc.cluster.local','rootCAs':['/tls/ca.crt']}}}}
 (gateway/'dynamic.yaml').write_text(yaml.safe_dump(dynamic))
 proxyname=start('s3-proxy','docker.io/library/traefik@sha256:1336e85aaed293bd63d005086e0dd013b04078b26aaeb1c18b26a2f928af7e65',['/usr/local/bin/traefik','--configFile=/configuration/static.yaml'],ports=[9443],folder='s3-proxy')
 peer=next(iter(json.loads(docker(['inspect',proxyname]))[0]['NetworkSettings']['Networks'].values()))['IPAddress']
 (prepared/'security.toml').write_text(m.PREPARE.proxy_config([peer+'/32'])+m.PREPARE.admin_config('synthetic-owner',pw))
 docker(['restart',name],timeout=40)
 import boto3
 from botocore.config import Config
 client=boto3.client('s3',endpoint_url='https://127.0.0.1:9443/s3',region_name='us-east-1',aws_access_key_id=credential['accessKey'],aws_secret_access_key=credential['secretKey'],verify=str(d/'tls/ca.crt'),config=Config(signature_version='s3v4',s3={'addressing_style':'path'},retries={'max_attempts':0},connect_timeout=3,read_timeout=10))
 response=None
 for attempt in range(40):
  try:
   response=session.get('https://127.0.0.1:9443/s3/',verify=str(d/'tls/ca.crt'),timeout=3)
   if response.status_code in [200,403]:break
  except requests.RequestException:pass
  time.sleep(1)
 check('S3-native-ready-before-write',response is not None and response.status_code in [200,403],status=response.status_code if response is not None else None)
 client.create_bucket(Bucket='synthetic-bucket');payload=b'synthetic payload'
 client.put_object(Bucket='synthetic-bucket',Key='synthetic/object.txt',Body=payload)
 result=client.get_object(Bucket='synthetic-bucket',Key='synthetic/object.txt');check('S3-SigV4-prefix-write-read',result['Body'].read()==payload)
 listed=client.list_objects_v2(Bucket='synthetic-bucket',Prefix='synthetic/');check('S3-prefix-list',any(o['Key']=='synthetic/object.txt'for o in listed.get('Contents',[])))
 signed=client.generate_presigned_url('get_object',Params={'Bucket':'synthetic-bucket','Key':'synthetic/object.txt'},ExpiresIn=60)
 result=session.get(signed,verify=str(d/'tls/ca.crt'),timeout=10);check('S3-presigned-prefix',result.status_code==200 and result.content==payload,status=result.status_code)
 upload=client.create_multipart_upload(Bucket='synthetic-bucket',Key='synthetic/multipart.bin');part=client.upload_part(Bucket='synthetic-bucket',Key='synthetic/multipart.bin',UploadId=upload['UploadId'],PartNumber=1,Body=payload)
 client.complete_multipart_upload(Bucket='synthetic-bucket',Key='synthetic/multipart.bin',UploadId=upload['UploadId'],MultipartUpload={'Parts':[{'PartNumber':1,'ETag':part['ETag']}]})
 result=client.get_object(Bucket='synthetic-bucket',Key='synthetic/multipart.bin');check('S3-multipart-prefix',result['Body'].read()==payload)
 client.put_object(Bucket='synthetic-bucket',Key='synthetic/space + unicode-ä.txt',Body=payload)
 result=client.get_object(Bucket='synthetic-bucket',Key='synthetic/space + unicode-ä.txt');check('S3-encoded-object-key',result['Body'].read()==payload)
 wrong=boto3.client('s3',endpoint_url='https://127.0.0.1:9443/s3',region_name='us-east-1',aws_access_key_id=credential['accessKey'],aws_secret_access_key='invalid-synthetic-key',verify=str(d/'tls/ca.crt'),config=Config(signature_version='s3v4',s3={'addressing_style':'path'},retries={'max_attempts':0}))
 try:wrong.head_bucket(Bucket='synthetic-bucket')
 except Exception as error:check('S3-invalid-key-denied',getattr(error,'response',{}).get('ResponseMetadata',{}).get('HTTPStatusCode')==403)
 else:check('S3-invalid-key-denied',False)
 response=session.get('https://127.0.0.1:9443/s3/synthetic-bucket/synthetic/object.txt',verify=str(d/'tls/ca.crt'),timeout=5);check('S3-anonymous-denied',response.status_code==403,status=response.status_code)
 result=session.get(signed,headers={'X-Forwarded-Prefix':'/forged','Cookie':'platform-session=synthetic'},verify=str(d/'tls/ca.crt'),timeout=10);check('S3-client-prefix-cannot-override-proxy',result.status_code==200 and result.content==payload,status=result.status_code)
 docker(['restart',name],timeout=40)
 response=None
 for attempt in range(40):
  try:
   response=session.get(signed,verify=str(d/'tls/ca.crt'),timeout=3)
   if response.status_code==200:break
  except requests.RequestException:pass
  time.sleep(1)
 check('S3-restart-preserves-object',response is not None and response.status_code==200 and response.content==payload)
 print(json.dumps({'directory':str(d),'checks':receipt['checks'],'form_fields':receipt['form_fields'],'logout_links':receipt['logout_links']}),flush=True)

except Exception as error:
 receipt['failure_type']=type(error).__name__
 print('Native portable contract failed; inspect the private synthetic receipt',flush=True)
 raise SystemExit(1)from None
finally:
 for name in reversed(names):
  logs=subprocess.run(['docker','logs','--tail','80',name],env=env,capture_output=True,timeout=10)
  (d/(name+'.log')).write_bytes(logs.stdout+logs.stderr);(d/(name+'.log')).chmod(0o600)
  subprocess.run(['docker','stop','-t','3',name],env=env,capture_output=True,timeout=15)
  subprocess.run(['docker','rm',name],env=env,capture_output=True,timeout=10)
 for p in prepared.iterdir():p.chmod(0o600)
 d.chmod(0o700)
 (state/'qualification.json').write_text(json.dumps(receipt));(state/'qualification.json').chmod(0o600)
