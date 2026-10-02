#!/usr/bin/env python3
"""External, single-project task executor. API process never executes repository code."""
import json, os, pathlib, signal, subprocess, sys, tempfile, time, urllib.error, urllib.request

URL=os.environ['FORGE_URL'].rstrip('/')
WORKER=os.environ.get('WORKER_ID','local-task-executor')
PROJECT=int(os.environ['GITLAB_PROJECT_ID'])
GITLAB=os.environ['GITLAB_URL'].rstrip('/')
ROOT=pathlib.Path(os.environ.get('WORKSPACE_ROOT','/workspace'))
ROOT.mkdir(parents=True,exist_ok=True)
OPENER=urllib.request.build_opener(urllib.request.ProxyHandler({}))
SOURCE='internal/pkg/storagepath/storagepath.go'
TEST='internal/pkg/storagepath/storagepath_test.go'
STOP=False

def api(path,data):
 req=urllib.request.Request(URL+path,data=json.dumps(data).encode(),headers={'Authorization':'Bearer '+os.environ['SERVICE_TOKEN'],'Content-Type':'application/json'})
 with OPENER.open(req,timeout=15) as response:return json.load(response)
def gate(job,head=''):
 return api('/internal/v1/tasks/'+job['task']['id']+'/gate',{'runId':job['id'],'epoch':job['epoch'],'worker':WORKER,'head':head})
def run(command,repo,code=False):
 env=os.environ.copy();env['GIT_TERMINAL_PROMPT']='0';env['GIT_ASKPASS']=str(pathlib.Path(__file__).with_name('askpass.py'))
 if code:
  for key in ['SERVICE_TOKEN','GITLAB_TOKEN','OIDC_CLIENT_SECRET','CASDOOR_SECRET','APP_KEY']:
   env.pop(key,None)
 r=subprocess.run(command,cwd=repo,env=env,text=True,capture_output=True,timeout=180)
 if r.returncode:raise RuntimeError((r.stdout+r.stderr)[-10000:])
 return r.stdout.strip()
def git(repo,*args):return run(['git','-c','credential.helper=',*args],repo)
def private_state(path,data):
 temporary=path.with_suffix('.tmp');temporary.write_text(json.dumps(data,ensure_ascii=False,indent=2));temporary.replace(path)
def work(job,output):
 t=job['task'];repo=ROOT/t['id'];remote=GITLAB+'/'+os.environ['GITLAB_PROJECT_PATH']+'.git'
 result={'status':'Failed','head':t['head'],'output':{}}
 try:
  if job['gitlabProjectId']!=PROJECT:raise RuntimeError('Executor project scope mismatch')
  if not repo.exists():run(['git','clone','--no-hardlinks',remote,str(repo)],ROOT)
  gate(job)
  git(repo,'fetch','origin',t['branch'])
  current=git(repo,'rev-parse','FETCH_HEAD')
  if current!=t['head']:raise RuntimeError('Remote commit changed before execution')
  if git(repo,'status','--porcelain'):raise RuntimeError('Uncommitted changes preserved; human intervention required')
  git(repo,'checkout','-B',t['branch'],t['head'])
  if not (repo/'.ai-review-gate-installed').exists():
   run([sys.executable,'setup_pre_commit.py'],repo)
  schema={'type':'object','properties':{'summary':{'type':'string'},'edits':{'type':'array','minItems':1,'maxItems':8,'items':{'type':'object','properties':{'path':{'type':'string','enum':[SOURCE,TEST]},'before':{'type':'string'},'after':{'type':'string'}},'required':['path','before','after'],'additionalProperties':False}}},'required':['summary','edits'],'additionalProperties':False}
  request={'task':t['title'],'description':t['description'],'criteria':t['criteria'],'formalRequirements':job['requirements'],'files':{p:(repo/p).read_text() for p in [SOURCE,TEST]},'rules':(repo/'AGENTS.md').read_text(),'allowedFiles':[SOURCE,TEST]}
  began=time.monotonic();passed=False;logs=[];summary=''
  for attempt in range(1,4):
   gate(job)
   payload={'model':os.environ['MODEL_NAME'],'stream':False,'think':False,'format':schema,'messages':[{'role':'system','content':'Repair only the explicitly allowed files. Return JSON with small localized edits: path, before (exact existing nonempty text that occurs once), after (replacement). Copy unchanged text exactly. Do not return whole files. Follow existing public interfaces and task acceptance. No unrelated edits. Never remove or weaken existing tests.'},{'role':'user','content':json.dumps(request,ensure_ascii=False)}],'options':{'temperature':0,'num_ctx':16384,'num_predict':8192}}
   req=urllib.request.Request(os.environ['MODEL_URL'].rstrip('/')+'/api/chat',data=json.dumps(payload).encode(),headers={'Content-Type':'application/json'})
   with OPENER.open(req,timeout=600) as response:answer=json.load(response)
   private_state(repo.parent/(job['id']+'-model-'+str(attempt)+'.json'),answer)
   gate(job)
   if git(repo,'ls-remote','origin','refs/heads/'+t['branch']).split()[0]!=t['head']:raise RuntimeError('Remote human commit takes priority')
   change=json.loads(answer['message']['content']);paths=[];updated={}
   try:
    for f in change['edits']:
     if f['path'] not in [SOURCE,TEST]:raise RuntimeError('Model file scope rejected')
     p=repo/f['path']
     if p.is_symlink():raise RuntimeError('Symbolic link write rejected')
     current=updated.get(f['path'],p.read_text())
     if not f['before'] or current.count(f['before'])!=1:raise RuntimeError('Edit must match one exact existing location')
     updated[f['path']]=current.replace(f['before'],f['after'],1)
     if f['path'] not in paths:paths.append(f['path'])
    if TEST in updated:
     old=[x for x in (repo/TEST).read_text().splitlines() if x.strip()]
     new=[x for x in updated[TEST].splitlines() if x.strip()]
     # Existing tests must remain byte-for-byte; additions are allowed.
     at=0
     for line in new:
      if at<len(old) and line==old[at]:at+=1
     if at!=len(old):raise RuntimeError('Existing test content changed; preserve all old cases exactly')
   except (KeyError,RuntimeError) as e:
    logs.append({'attempt':attempt,'passed':False,'log':str(e)})
    request['failure']=str(e);continue
   for path,content in updated.items():(repo/path).write_text(content)
   run(['gofmt','-w',*paths],repo,code=True)
   check=subprocess.run(['go','test','-count=1','./internal/pkg/storagepath'],cwd=repo,env={k:v for k,v in os.environ.items() if k not in ['SERVICE_TOKEN','GITLAB_TOKEN']},text=True,capture_output=True,timeout=180)
   logs.append({'attempt':attempt,'passed':check.returncode==0,'log':(check.stdout+check.stderr)[-10000:]})
   gate(job)
   if check.returncode==0:passed=True;summary=change['summary'];break
   if time.monotonic()-began>=1800:break
   request['failure']=logs[-1]['log'];request['files']={p:(repo/p).read_text() for p in [SOURCE,TEST]}
  result['output']={'summary':summary,'checks':logs,'durationSeconds':round(time.monotonic()-began,2),'patch':git(repo,'diff','--',SOURCE,TEST)}
  if not passed:raise RuntimeError('Repair attempt limit reached; patch preserved')
  run(['go','vet','./internal/pkg/storagepath'],repo,code=True);git(repo,'diff','--check')
  if not git(repo,'diff','--name-only'):raise RuntimeError('No code changes generated')
  gate(job)
  git(repo,'add',SOURCE,TEST)
  git(repo,'-c','user.name=Forge Task Executor','-c','user.email=forge-task-executor@localhost','commit','-m','fix: '+t['title'][:70]+' (task '+t['id']+')')
  head=git(repo,'rev-parse','HEAD');gate(job,head)
  # An atomic remote compare prevents a human commit from being replaced.
  git(repo,'push','--force-with-lease=refs/heads/'+t['branch']+':'+t['head'],'origin','HEAD:refs/heads/'+t['branch'])
  result['status']='Passed';result['head']=head;result['output']['checksCommit']=head
 except Exception as e:
  message=str(e) if not isinstance(e,urllib.error.HTTPError) else 'Execution gate or upstream service rejected the request'
  # Scrub credentials even if an external command echoed its environment.
  for key in ['SERVICE_TOKEN','GITLAB_TOKEN']:
   if os.environ.get(key):message=message.replace(os.environ[key],'[REDACTED]')
  result['output']['error']=message[-5000:]
 finally:private_state(output,result)

def stop_signal(*_):
 global STOP
 STOP=True

def main():
 signal.signal(signal.SIGTERM,stop_signal);signal.signal(signal.SIGINT,stop_signal)
 while not STOP:
  try:job=api('/internal/v1/tasks/claim',{'worker':WORKER,'gitlabProjectId':PROJECT})['run']
  except Exception:time.sleep(2);continue
  if not job:time.sleep(1);continue
  output=ROOT/(job['id']+'-result.json');jobfile=ROOT/(job['id']+'-job.json');private_state(jobfile,job)
  child=subprocess.Popen([sys.executable,__file__,'--job',str(jobfile),'--output',str(output)],start_new_session=True)
  halted=False;started=time.monotonic()
  while child.poll() is None:
   try:
    if STOP or time.monotonic()-started>=1800:raise RuntimeError('Execution stopped')
    gate(job)
   except Exception:
    halted=True;os.killpg(child.pid,signal.SIGTERM)
    try:child.wait(timeout=5)
    except subprocess.TimeoutExpired:os.killpg(child.pid,signal.SIGKILL);child.wait()
    break
   time.sleep(.5)
  child.wait()
  result={'status':'Stopped','head':job['task']['head'],'output':{'stopConfirmed':True}} if halted else json.loads(output.read_text()) if output.exists() else {'status':'Failed','head':job['task']['head'],'output':{'error':'Executor exited without result'}}
  finish={'runId':job['id'],'epoch':job['epoch'],'worker':WORKER,**result}
  deadline=time.monotonic()+60
  while time.monotonic()<deadline:
   try:api('/internal/v1/tasks/'+job['task']['id']+'/finish',finish);break
   except urllib.error.HTTPError as e:
    try:reason=json.load(e).get('error',{}).get('code','')
    except Exception:reason='UPSTREAM_ERROR'
    print(json.dumps({'runId':job['id'],'finishRejected':reason}),flush=True)
    if reason=='REMOTE_COMMIT_MISMATCH' and finish['status']!='Stopped':
     # GitLab updates MR diff refs asynchronously after accepting a push.
     # Continue only while this round still has write authority.
     try:gate(job)
     except Exception:finish['status']='Stopped';finish['output']['stopConfirmed']=True
    elif e.code==409:
     finish['status']='Stopped';finish['output']['stopConfirmed']=True
    time.sleep(1)
   except Exception:time.sleep(1)
  print(json.dumps({'taskId':job['task']['id'],'runId':job['id'],'status':finish['status']},ensure_ascii=False),flush=True)
if __name__=='__main__':
 if '--job' in sys.argv:
  j=pathlib.Path(sys.argv[sys.argv.index('--job')+1]);o=pathlib.Path(sys.argv[sys.argv.index('--output')+1]);work(json.loads(j.read_text()),o)
 else:main()
