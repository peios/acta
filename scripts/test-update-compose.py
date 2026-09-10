#!/usr/bin/env python3
"""Real signed-release update/recovery test in a disposable Compose installation.
Requires two signed release files, the matching public key, docker registry login,
and bin/acta2-update. Never targets an existing deployment. Retains failed stacks
only with KEEP_UPDATE_TEST=1. Signing fixtures use a new test-only key.
"""
import argparse,base64,hashlib,json,os,secrets,shutil,subprocess,tempfile,time,uuid
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--current',type=Path,required=True);p.add_argument('--target',type=Path,required=True)
p.add_argument('--bootstrap-controller',action='store_true',help='Test initial pre-production app transition using the candidate controller')
p.add_argument('--public-key',type=Path,default=Path('deploy/update/release.pub'))
a=p.parse_args();repo=Path(__file__).resolve().parents[1]
root=Path(tempfile.mkdtemp(prefix='acta-update-test-'));project='acta-update-test-'+uuid.uuid4().hex[:8]
binary=repo/'bin/acta2-update';config=root/'config';state=root/'state'
def run(args,**kw):return subprocess.run(list(map(str,args)),cwd=repo,check=True,text=True,**kw)
def out(args):return run(args,stdout=subprocess.PIPE).stdout.strip()
def decoded(path):return json.loads(out([binary,'-input',path,'-public-key',a.public_key,'verify']))
old,new=decoded(a.current),decoded(a.target)
assert new['sequence']>old['sequence']
if a.bootstrap_controller:old['images']['updater']=new['images']['updater']
run(['python3','scripts/configure-deployment.py','--domain','acta.test','--email','test@example.com','--directory',config,'--project',project])
run([binary,'-key',root/'test.seed','-public-key',root/'test.pub','keygen'])
run(['python3','scripts/configure-updates.py','--deployment',config,'--bundle',repo,'--state',state,'--public-key',root/'test.pub','--prereleases'])
registry=Path(os.environ.get('DOCKER_CONFIG',str(Path.home()/'.docker')))/'config.json'
if registry.is_file():shutil.copy2(registry,config/'registry/config.json')
# Re-sign the exact verified production image sets for our isolated test trust root.
def sign(r,label):
 source=root/(label+'.manifest.json');source.write_text(json.dumps(r));dest=root/(label+'.signed.json')
 run([binary,'-key',root/'test.seed','-input',source,'-output',dest,'sign']);return json.loads(dest.read_text()),dest
old_signed,old_path=sign(old,'old');new_signed,new_path=sign(new,'new')
run([binary,'-config',config/'update/config.json','-input',old_path,'bootstrap'])
repository=root/'repository';repository.mkdir()
with (config/'.env').open('a') as f:f.write(f'ACTA_BACKUP_REPOSITORY={repository}\n')
operator=config/'backup';operator.mkdir()
for image in set(old['images'].values())|set(new['images'].values()):run(['docker','pull',image])
age=out(['docker','run','--rm','--entrypoint','age-keygen',old['images']['backup']])
(operator/'recovery.age').write_text(age+'\n')
recipient=next(l.removeprefix('# public key: ') for l in age.splitlines() if l.startswith('# public key: '))
backup_config=json.loads((repo/'deploy/production/backup.example.json').read_text());backup_config['recovery_recipient']=recipient
(operator/'backup.json').write_text(json.dumps(backup_config))
(operator/'database-url').write_text('postgres://postgres@/acta2?host=/var/run/postgresql')
(operator/'pgbackrest.conf').write_text((repo/'deploy/production/pgbackrest.example.conf').read_text().replace('REPLACE_WITH_A_RANDOM_SECRET',secrets.token_hex(32)))
for path in operator.iterdir():path.chmod(0o600)
(config/'Caddyfile').write_text((repo/'deploy/production/Caddyfile').read_text().replace('{$ACTA_DOMAIN} {','{$ACTA_DOMAIN} {\n tls internal'))
(config/'review.yaml').write_text(f'''services:
  app:
    healthcheck:
      interval: 1s
      start_period: 1s
      retries: 5
  caddy:
    ports: !override
      - "127.0.0.1::80"
      - "127.0.0.1::443"
    volumes:
      - {config}/Caddyfile:/etc/caddy/Caddyfile:ro
''')
c=json.loads((config/'update/config.json').read_text());c['compose_files'].append(str(config/'review.yaml'));(config/'update/config.json').write_text(json.dumps(c))
compose=(['sudo','-n'] if os.environ.get('GITHUB_ACTIONS')=='true' else [])+['docker','compose','--project-name',project,'--env-file',config/'.env']
for file in c['compose_files']:compose+=['-f',file]
compose+=['-f',state/'active.json','--profile','backups','--profile','updates']
def dc(*args):return out([*compose,*args])
def control(*args):return json.loads(dc('exec','-T','updater','acta2-update','-config','/etc/acta-update/config.json',*args))
def sql(q):return dc('exec','-T','-u','postgres','db','psql','-U','postgres','-d','acta2','-At','-v','ON_ERROR_STOP=1','-c',q)
def wait(fn,label,seconds=600):
 until=time.monotonic()+seconds
 while time.monotonic()<until:
  try:
   value=fn()
   if value:return value
  except (subprocess.CalledProcessError,json.JSONDecodeError,KeyError):pass
  time.sleep(1)
 raise AssertionError('Timed out: '+label)
def journal():
 # Updater-owned files stay private. Use a read-only mount via trusted image.
 return json.loads(out(['docker','run','--rm','--network','none','--entrypoint','cat','-v',f'{state}:/state:ro',new['images']['updater'],'/state/state.json']))
def offer(signed):
 dc('stop','updater')
 # Fault-injection only: patch the paused journal, never bypass signature checks.
 path=root/'offer.json';path.write_text(json.dumps(signed))
 helper='import json,os;from pathlib import Path;p=Path("/state/state.json");s=json.loads(p.read_text());s["available"]=json.loads(Path("/offer").read_text());p.write_text(json.dumps(s));p.chmod(0o600)'
 run(['docker','run','--rm','--network','none','--entrypoint','python3','-v',f'{state}:/state','-v',f'{path}:/offer:ro',new['images']['backup'],'-c',helper])
 dc('up','-d','--no-build','--no-deps','updater')
 wait(lambda:control('status'),'updater socket')
 return hashlib.sha256(signed['payload'].encode()).hexdigest()
def finished():
 j=control('status')['jobs'][0]
 if j['phase'] in ('succeeded','rolled_back','failed'):return j
 return None
try:
 # Prepare worker configuration before starting the archive-enabled database.
 dc('up','-d','--no-build','--no-deps','backup')
 dc('exec','-T','backup','chown','999:10001','/repository')
 dc('up','-d','--no-build','--wait','--wait-timeout','300','db','app','caddy','backup','updater')
 dc('exec','-T','-u','999:10001','backup','pgbackrest','--config=/var/lib/acta-backup/config/pgbackrest.conf','--stanza=acta2','stanza-create')
 sql("CREATE TABLE update_probe(id int PRIMARY KEY, value text NOT NULL); INSERT INTO update_probe VALUES(1,'preserve this')")
 selected=offer(new_signed)
 control('-release-id',selected,'install')
 # Restart the independent controller while the backup phase is underway.
 dc('restart','updater')
 result=wait(finished,'successful release update',1200)
 assert result['phase']=='succeeded',result
 assert sql('SELECT value FROM update_probe WHERE id=1')=='preserve this'
 assert control('status')['current']['sequence']==new['sequence']
 print('PASS: signed update, encrypted full backup + restore drill, updater restart, data preservation',flush=True)
 # Build a deliberately broken migration fixture using a test signing key.
 # It commits a schema change and then fails; recovery must restore the data,
 # not merely swap the image. It is never published as a GitHub release.
 fixture=root/'failure';fixture.mkdir()
 (fixture/'main.go').write_text('''package main
import("context";"os";"time";"github.com/jackc/pgx/v5")
func main(){c,e:=pgx.Connect(context.Background(),os.Getenv("ACTA_DATABASE_URL"));if e!=nil{os.Exit(41)};_,e=c.Exec(context.Background(),"CREATE TABLE IF NOT EXISTS update_failure(id int)");if e!=nil{os.Exit(43)};time.Sleep(5*time.Second);_,_=c.Exec(context.Background(),"SELECT 1/0");os.Exit(42)}
''')
 run(['docker','run','--rm','-v',f'{fixture}:/fixture','acta2-release-tools','go','build','-o','/fixture/fail','/fixture/main.go'])
 (fixture/'Dockerfile').write_text('FROM '+new['images']['app']+'\nCOPY fail /usr/local/bin/acta2-server\n')
 tag='ghcr.io/peios/acta2-app:qa-failure-'+uuid.uuid4().hex[:10]
 run(['docker','buildx','build','--platform','linux/amd64','--push','-t',tag,fixture])
 digest=json.loads(out(['docker','buildx','imagetools','inspect',tag,'--format','{{json .Manifest.Digest}}']))
 broken=json.loads(json.dumps(new));broken['images']['app']=tag.split(':')[0]+'@'+digest
 broken['version']='v0.0.0-qa-failure';broken['sequence']=new['sequence']+1
 for crash in (False,True):
  sql("INSERT INTO accounts(id,username) VALUES('11111111-1111-4111-8111-111111111111','recovery-test') ON CONFLICT DO NOTHING; INSERT INTO browser_sessions(token_hash,account_id,authorized_by,created_at,last_seen_at,expires_at) VALUES(decode(repeat('ab',32),'hex'),'11111111-1111-4111-8111-111111111111','11111111-1111-4111-8111-111111111111',now(),now(),now()+interval '1 day')")
  broken_signed,_=sign(broken,'failure-'+str(crash));selected=offer(broken_signed)
  control('-release-id',selected,'install')
  if crash:
   wait(lambda: control('status')['jobs'][0]['phase']=='applying','candidate phase',1200)
   wait(lambda:sql("SELECT to_regclass('update_failure') IS NOT NULL")=='t','candidate migration write')
   # Simulate loss of all application containers in this disposable project.
   ids=dc('ps','-q').splitlines()
   run(['docker','update','--restart=no',*ids])
   run(['docker','kill',*ids])
   for service in ('db','app','backup','caddy','updater'):
    run(['docker','start',dc('ps','-a','-q',service)])
  result=wait(finished,'failed migration recovery',1200)
  assert result['phase']=='rolled_back',result
  assert sql('SELECT value FROM update_probe WHERE id=1')=='preserve this'
  assert sql("SELECT to_regclass('update_failure') IS NULL")=='t'
  assert control('status')['current']['sequence']==new['sequence']
  assert sql('SELECT count(*) FROM browser_sessions')=='0'
  print('PASS: failed migration recovery'+(' after whole-stack interruption' if crash else ''),flush=True)
  broken['sequence']+=1
 print('PASS: real release update and both recovery paths',flush=True)
except BaseException:
 for service in ('updater','app','db','backup'):
  ids=out(['docker','ps','-aq','--filter','label=com.docker.compose.project='+project,'--filter','label=com.docker.compose.service='+service]).splitlines()
  for cid in ids:subprocess.run(['docker','logs','--tail','180',cid],check=False)
 raise
finally:
 if os.environ.get('KEEP_UPDATE_TEST')=='1':print('Retained',root,project,flush=True)
 else:
  helpers=out(['docker','container','ls','-aq','--filter','label=acta.update.installation='+c['installation']]).splitlines()
  if helpers:run(['docker','rm','-f',*helpers])
  subprocess.run(list(map(str,[*compose,'down','--volumes','--remove-orphans'])),cwd=repo,check=False)
  snapshots=out(['docker','volume','ls','-q','--filter','label=acta.update.installation='+c['installation']]).splitlines()
  for volume in snapshots:run(['docker','volume','rm',volume])
  run(['docker','run','--rm','--network','none','--entrypoint','sh','-v',f'{root}:/cleanup',new['images']['backup'],'-c',f'chown -R {os.getuid()}:{os.getgid()} /cleanup'])
  shutil.rmtree(root)
