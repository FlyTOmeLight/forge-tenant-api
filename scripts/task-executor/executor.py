#!/usr/bin/env python3
"""External, single-project task executor. API process never executes repository code."""
import fnmatch, json, os, pathlib, shlex, signal, subprocess, sys, time, urllib.error, urllib.request

URL=os.environ['FORGE_URL'].rstrip('/')
WORKER=os.environ.get('WORKER_ID','local-task-executor')
PROJECT=int(os.environ['GITLAB_PROJECT_ID'])
GITLAB=os.environ['GITLAB_URL'].rstrip('/')
ROOT=pathlib.Path(os.environ.get('WORKSPACE_ROOT','/workspace'))
ROOT.mkdir(parents=True,exist_ok=True)
OPENER=urllib.request.build_opener(urllib.request.ProxyHandler({}))
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
def gitlab(method,path,data=None):
 req=urllib.request.Request(GITLAB+'/api/v4'+path,data=json.dumps(data).encode() if data is not None else None,method=method,headers={'PRIVATE-TOKEN':os.environ['GITLAB_TOKEN'],'Content-Type':'application/json'})
 with OPENER.open(req,timeout=20) as response:return json.load(response)
def model(job,request,schema,system,suffix):
 config=job['config']
 req=urllib.request.Request(os.environ['MODEL_URL'].rstrip('/')+'/api/chat',data=json.dumps({'model':config['model'],'stream':False,'think':False,'format':schema,'messages':[{'role':'system','content':system},{'role':'user','content':json.dumps(request,ensure_ascii=False)}],'options':{'temperature':0,'num_ctx':16384,'num_predict':8192}}).encode(),headers={'Content-Type':'application/json'})
 with OPENER.open(req,timeout=600) as response:answer=json.load(response)
 private_state(ROOT/(job['id']+'-model-'+suffix+'.json'),answer)
 gate(job)
 return json.loads(answer['message']['content'])
def in_scope(path,scopes):
 p=pathlib.PurePosixPath(path)
 if p.is_absolute() or '..' in p.parts or '.git' in p.parts or '\\' in path:return False
 return any(path==scope.rstrip('/') or path.startswith(scope.rstrip('/')+'/') or fnmatch.fnmatchcase(path,scope) for scope in scopes)
def checked_commands(job,repo):
 results=[]
 env={k:v for k,v in os.environ.items() if k not in ['SERVICE_TOKEN','GITLAB_TOKEN','APP_KEY','CASDOOR_SECRET','OIDC_CLIENT_SECRET']}
 for command in job['config']['checks']:
  gate(job)
  r=subprocess.run(shlex.split(command),cwd=repo,env=env,text=True,capture_output=True,timeout=180)
  results.append({'command':command,'exitCode':r.returncode,'passed':r.returncode==0,'log':(r.stdout+r.stderr)[-10000:]})
 return results
def remote_head(repo,branch):
 response=git(repo,'ls-remote','origin','refs/heads/'+branch)
 return response.split()[0] if response else ''
def work(job,output):
 t=job['task'];c=job['config'];repo=ROOT/t['id'];remote=GITLAB+'/'+job['projectPath']+'.git'
 result={'status':'Failed','head':t['head'],'mrIid':t['mrIid'],'output':{}}
 began=time.monotonic();logs=[];touched=set()
 try:
  if PROJECT and job['gitlabProjectId']!=PROJECT:raise RuntimeError('Executor project scope mismatch')
  if not c.get('allowedPaths') or not c.get('checks') or not c.get('model'):raise RuntimeError('Project execution configuration is incomplete')
  if not repo.exists():run(['git','clone','--no-hardlinks',remote,str(repo)],ROOT)
  gate(job)
  old_remote=remote_head(repo,t['branch'])
  if old_remote and old_remote!=t['head']:raise RuntimeError('Remote commit changed before execution')
  if not old_remote and t['mrIid']:raise RuntimeError('Existing MR branch is unavailable')
  if git(repo,'status','--porcelain'):raise RuntimeError('Uncommitted changes preserved; human intervention required')
  git(repo,'fetch','origin',t['head']);git(repo,'checkout','-B',t['branch'],t['head'])
  rules={}
  if (repo/'AGENTS.md').exists():rules['AGENTS.md']=(repo/'AGENTS.md').read_text()
  if (repo/'setup_pre_commit.py').exists() and not (repo/'.ai-review-gate-installed').exists():run([sys.executable,'setup_pre_commit.py'],repo)
  files=[p for p in git(repo,'ls-files').splitlines() if in_scope(p,c['allowedPaths']) and not (repo/p).is_symlink()]
  if not files:raise RuntimeError('No tracked source files in the configured modification scope')
  if len(files)>12:
   plan=model(job,{'task':t['title'],'description':t['description'],'requirements':job['requirements'],'files':files}, {'type':'object','properties':{'files':{'type':'array','minItems':1,'maxItems':12,'items':{'type':'string','enum':files}}},'required':['files'],'additionalProperties':False},'Choose the source and neighboring tests needed to diagnose this single bug. Return only file paths.','plan')
   files=plan['files']
  if len(files)>12 or sum((repo/p).stat().st_size for p in files)>98304:raise RuntimeError('Selected source exceeds one-round context capacity; narrow the project scope')
  for f in files:
   for ancestor in pathlib.PurePosixPath(f).parents:
    path=(ancestor/'AGENTS.md').as_posix()
    if path not in rules and (repo/path).is_file():rules[path]=(repo/path).read_text()
  baseline=checked_commands(job,repo);result['output']['baseline']=baseline
  schema={'type':'object','properties':{'summary':{'type':'string'},'edits':{'type':'array','minItems':1,'maxItems':12,'items':{'type':'object','properties':{'path':{'type':'string'},'before':{'type':'string'},'after':{'type':'string'}},'required':['path','before','after'],'additionalProperties':False}}},'required':['summary','edits'],'additionalProperties':False}
  request={'task':t['title'],'description':t['description'],'reproduce':t.get('reproduce',''),'criteria':t['criteria'],'constraints':t.get('constraints',''),'formalRequirements':job['requirements'],'files':{p:(repo/p).read_text() for p in files},'rules':rules,'allowedPaths':c['allowedPaths'],'baseline':baseline}
  passed=False;summary=''
  for attempt in range(1,c['repairLimit']+1):
   gate(job)
   change=model(job,request,schema,'Repair the stated bug within allowedPaths. Return small localized edits: path, before (exact existing nonempty text occurring once), after (replacement). For a new file use empty before. Preserve existing public interfaces and existing tests exactly. Add regression tests where needed. Follow repository AGENTS instructions. No unrelated edits.',str(attempt))
   if remote_head(repo,t['branch'])!=old_remote:raise RuntimeError('Remote human commit takes priority')
   updated={};paths=[]
   try:
    for f in change['edits']:
     if not in_scope(f['path'],c['allowedPaths']):raise RuntimeError('Model file scope rejected')
     p=repo/f['path']
     if p.is_symlink() or not p.resolve().is_relative_to(repo.resolve()):raise RuntimeError('Unsafe file write rejected')
     current=updated.get(f['path'],p.read_text() if p.exists() else '')
     if f['before']:
      if current.count(f['before'])!=1:raise RuntimeError('Edit must match one exact existing location')
      current=current.replace(f['before'],f['after'],1)
     else:
      if p.exists() or f['path'] in updated:raise RuntimeError('New-file edit cannot replace an existing file')
      current=f['after']
     updated[f['path']]=current
     if f['path'] not in paths:paths.append(f['path'])
    for path,content in updated.items():
     name=pathlib.PurePosixPath(path).name
     if (name.endswith('_test.go') or name.startswith('test_') or '.test.' in name or '.spec.' in name) and (repo/path).exists():
      old=[x for x in (repo/path).read_text().splitlines() if x.strip()];new=[x for x in content.splitlines() if x.strip()];at=0
      for line in new:
       if at<len(old) and line==old[at]:at+=1
      if at!=len(old):raise RuntimeError('Existing test content changed; preserve all old cases exactly')
   except (KeyError,RuntimeError) as e:
    logs.append({'attempt':attempt,'passed':False,'log':str(e)});request['failure']=str(e);continue
   for path,content in updated.items():
    touched.add(path)
    p=repo/path;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(content)
    if path not in files:files.append(path)
   go_files=[p for p in paths if p.endswith('.go')]
   if go_files:run(['gofmt','-w',*go_files],repo,code=True)
   checks=checked_commands(job,repo)
   passed=all(x['passed'] for x in checks)
   logs.append({'attempt':attempt,'passed':passed,'commands':checks,'log':'\n'.join(x['command']+'\n'+x['log'] for x in checks)})
   gate(job)
   if passed:summary=change['summary'];break
   if time.monotonic()-began>=c['timeoutMinutes']*60:break
   request['failure']=logs[-1]['log'];request['files']={p:(repo/p).read_text() for p in files}
  result['output'].update({'summary':summary,'checks':logs,'durationSeconds':round(time.monotonic()-began,2),'patch':git(repo,'diff'),'configVersion':c['version']})
  if not passed:raise RuntimeError('Repair attempt limit reached; patch preserved')
  git(repo,'diff','--check')
  changed=git(repo,'status','--porcelain')
  if not changed:raise RuntimeError('No code changes generated; no empty MR created')
  if any(p not in touched for p in git(repo,'diff','--name-only').splitlines()):raise RuntimeError('Validation changed source outside the model edits')
  gate(job);git(repo,'add','--',*sorted(touched))
  result['output']['patch']=git(repo,'diff','--cached')
  judgment=model(job,{'criteria':t['criteria'],'constraints':t.get('constraints',''),'patch':result['output']['patch'],'checks':logs[-1]}, {'type':'object','properties':{'status':{'type':'string','enum':['Met','Unmet','NeedsHuman']},'summary':{'type':'string'}},'required':['status','summary'],'additionalProperties':False},'Assess the stated acceptance criteria from the actual patch and checks. Do not equate passing commands with all criteria being met. Use NeedsHuman where evidence is missing. Return your advisory judgment only; do not propose or execute edits.','review')
  result['output']['judgment']=judgment
  git(repo,'-c','user.name=Forge Task Executor','-c','user.email=forge-task-executor@localhost','commit','-m','fix: '+t['title'][:70]+' (task '+t['id']+')')
  head=git(repo,'rev-parse','HEAD');gate(job,head)
  git(repo,'push','--force-with-lease=refs/heads/'+t['branch']+':'+old_remote,'origin','HEAD:refs/heads/'+t['branch'])
  result['head']=head;result['output']['checksCommit']=head
  if not t['mrIid']:
   gate(job)
   mr=gitlab('POST','/projects/'+str(job['gitlabProjectId'])+'/merge_requests',{'source_branch':t['branch'],'target_branch':t['targetBranch'],'title':'Draft: '+t['title'],'description':'## 问题与复现\n'+t['description']+'\n'+t.get('reproduce','')+'\n## 验收条件\n'+t['criteria']+'\n## 本轮交付\n'+summary+'\n\n检查绑定 commit: `'+head+'`\n\n'+('人工注入联调案例，不计入真实 bug。' if t['synthetic'] else '')})
   result['mrIid']=mr['iid']
  result['status']='Passed'
 except Exception as e:
  message=str(e) if not isinstance(e,urllib.error.HTTPError) else 'Execution gate or upstream service rejected the request'
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
    if STOP or time.monotonic()-started>=job["config"]["timeoutMinutes"]*60:raise RuntimeError('Execution stopped')
    gate(job)
   except Exception:
    halted=True;os.killpg(child.pid,signal.SIGTERM)
    try:child.wait(timeout=5)
    except subprocess.TimeoutExpired:os.killpg(child.pid,signal.SIGKILL);child.wait()
    break
   time.sleep(.5)
  child.wait()
  result={'status':'Stopped','head':job['task']['head'],'mrIid':job['task']['mrIid'],'output':{'stopConfirmed':True}} if halted else json.loads(output.read_text()) if output.exists() else {'status':'Failed','head':job['task']['head'],'output':{'error':'Executor exited without result'}}
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
