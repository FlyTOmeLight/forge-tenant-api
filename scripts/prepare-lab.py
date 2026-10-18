"""Provision this application in the existing lab. Never print credentials."""
import argparse, base64, json, pathlib, secrets, subprocess, time, urllib.error, urllib.parse, urllib.request
p=argparse.ArgumentParser()
p.add_argument('--lab-config',required=True)
p.add_argument('--kubectl',default='kubectl')
p.add_argument('--namespace',default='forge-tenant-split')
p.add_argument('--public-url',default='http://127.0.0.1:8483')
p.add_argument('--oidc-client-id',default='forge-tenant-split-console')
a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
local=root/'.local'; local.mkdir(mode=0o700,exist_ok=True);local.chmod(0o700)
lab=json.loads(pathlib.Path(a.lab_config).read_text())
file=local/'deployment.json'
if file.exists(): config=json.loads(file.read_text())
else:
    config={'databasePassword':secrets.token_hex(24),'appKey':secrets.token_hex(32),'oidcSecret':secrets.token_hex(24),'adminPassword':secrets.token_urlsafe(24),'adminUsername':'forge-admin','publicURL':a.public_url}
    file.write_text(json.dumps(config,indent=2));file.chmod(0o600)
kube=[a.kubectl,'--context','orbstack']
forward=subprocess.Popen(kube+['-n','tenant-lab','port-forward','--address','127.0.0.1','service/casdoor','8500:8000'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
def cas(action,body=None,**query):
    req=urllib.request.Request('http://127.0.0.1:8500/api/'+action+'?'+urllib.parse.urlencode(query),data=json.dumps(body).encode() if body is not None else None,headers={'Authorization':'Basic '+base64.b64encode(('tenant-lab-management:'+lab['managementSecret']).encode()).decode(),'Content-Type':'application/json'})
    with opener.open(req,timeout=30) as r: result=json.load(r)
    if result.get('status')!='ok': raise RuntimeError('Casdoor rejected '+action)
    return result.get('data')
try:
    deadline=time.monotonic()+20
    while True:
        try: cas('get-application',id='admin/lab-gitlab');break
        except (urllib.error.URLError,ConnectionError):
            if time.monotonic()>deadline: raise RuntimeError('Casdoor port-forward unavailable')
            time.sleep(.2)
    app=cas('get-application',id='admin/'+a.oidc_client_id)
    if not app:
        cas('add-application',{'owner':'admin','name':a.oidc_client_id,'displayName':'Forge Tenant Console','organization':'lab-identities','clientId':a.oidc_client_id,'clientSecret':config['oidcSecret'],'cert':'cert-built-in','enablePassword':True,'enableSignUp':False,'enableSigninSession':True,'grantTypes':['authorization_code'],'redirectUris':[config['publicURL']+'/auth/callback'],'tokenFormat':'JWT','expireInHours':1,'cookieExpireInHours':1,'signinMethods':[{'name':'Password','displayName':'Password','rule':'All'}]})
    elif app.get('clientSecret')!=config['oidcSecret'] or config['publicURL']+'/auth/callback' not in app.get('redirectUris',[]):
        raise RuntimeError('Existing Forge OIDC application differs from local private configuration')
    user=cas('get-user',id='lab-identities/'+config['adminUsername'])
    if not user: cas('add-user',{'owner':'lab-identities','name':config['adminUsername'],'displayName':'Forge 管理员','email':'forge-admin@example.test','password':config['adminPassword'],'type':'normal-user','isAdmin':False,'isForbidden':False})
finally:
    forward.terminate();forward.wait(timeout=10)
ns={'apiVersion':'v1','kind':'Namespace','metadata':{'name':a.namespace}}
def apply(obj):subprocess.run(kube+['apply','-f','-'],input=json.dumps(obj).encode(),check=True,stdout=subprocess.DEVNULL)
apply(ns)
env={'DATABASE_URL':'postgres://forge:'+config['databasePassword']+'@forge-tenant-postgres:5432/forge?sslmode=disable','POSTGRES_PASSWORD':config['databasePassword'],'PUBLIC_URL':config['publicURL'],'OIDC_ISSUER':'https://identity.tenant-lab.svc.cluster.local:8080','OIDC_CLIENT_ID':a.oidc_client_id,'OIDC_CLIENT_SECRET':config['oidcSecret'],'CASDOOR_URL':'http://casdoor.tenant-lab.svc.cluster.local:8000','CASDOOR_CLIENT_ID':'tenant-lab-management','CASDOOR_SECRET':lab['managementSecret'],'CASDOOR_ORGANIZATION':'lab-identities','GITLAB_URL':'http://gitlab-webservice-default.tenant-lab.svc.cluster.local:8181','GITLAB_TOKEN':lab['gitlabToken'],'BOOTSTRAP_USERNAME':config['adminUsername'],'TENANT_ENCRYPTION_KEY':config['appKey'],'CA_FILE':'/ca/ca.crt'}
apply({'apiVersion':'v1','kind':'Secret','metadata':{'name':'forge-tenant-env','namespace':a.namespace},'type':'Opaque','stringData':env})
apply({'apiVersion':'v1','kind':'Secret','metadata':{'name':'forge-tenant-ca','namespace':a.namespace},'type':'Opaque','stringData':{'ca.crt':lab['caCert']}})
print('Forge identity application, dedicated administrator and Kubernetes Secret references prepared.')
