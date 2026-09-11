#!/usr/bin/env python3
"""Exercise the real production images in a disposable, loopback-only Compose stack.
Build first. Requires Docker Compose >=2.24.4, Python and curl. Never publishes,
uses public ACME, reads existing credentials, or contacts an existing Acta DB.
"""
from pathlib import Path
import hashlib
import json
import os
import shutil
import secrets
import subprocess
import tempfile
import time
import uuid

repo = Path(__file__).resolve().parents[1]
root = Path(tempfile.mkdtemp(prefix='acta-compose-test-', dir='/tmp'))
project = 'acta-compose-test-' + uuid.uuid4().hex[:10]

def run(args, **kwargs):
    return subprocess.run(args, cwd=repo, check=True, text=True, **kwargs)

def output(args):
    return run(args, stdout=subprocess.PIPE).stdout.strip()

run(['python3', 'scripts/configure-deployment.py', '--domain', 'acta.test', '--email',
     'test@example.com', '--directory', str(root/'config'), '--project', project])
(root/'Caddyfile').write_text((repo/'deploy/production/Caddyfile').read_text().replace(
    '{$ACTA_DOMAIN} {', '{$ACTA_DOMAIN} {\n tls internal'))
(root/'override.yaml').write_text(f'''services:
  caddy:
    ports: !override
      - "127.0.0.1::80"
      - "127.0.0.1::443"
    volumes:
      - {root}/Caddyfile:/etc/caddy/Caddyfile:ro
''')
compose = ['docker', 'compose', '--env-file', str(root/'config/.env'), '-f',
           str(repo/'compose.production.yaml'), '-f', str(root/'override.yaml')]
def dc(*args):
    return output([*compose, *args])
def wait(fn, label):
    until = time.monotonic() + 180
    while time.monotonic() < until:
        try:
            if fn(): return
        except (subprocess.CalledProcessError, ValueError):
            pass
        time.sleep(1)
    raise AssertionError('timed out: '+label)
def sql(query):
    return dc('exec', '-T', '-u', 'postgres', 'db', 'psql', '-U', 'postgres', '-d', 'acta', '-Atc', query)
try:
    run([*compose, 'up', '-d', '--no-build', '--wait', '--wait-timeout', '180'])
    caddy = dc('ps', '-q', 'caddy')
    http_port = dc('port', 'caddy', '80').split(':')[-1]
    tls_port = dc('port', 'caddy', '443').split(':')[-1]
    wait(lambda: run(['docker','cp',caddy+':/data/caddy/pki/authorities/local/root.crt',str(root/'root.crt')],
                    stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0, 'local TLS CA')
    curl = ['curl','--noproxy','*','--silent','--show-error','--fail','--max-time','10',
            '--cacert',str(root/'root.crt'),'--connect-to',f'acta.test:443:127.0.0.1:{tls_port}']
    wait(lambda: bool(output([*curl,'https://acta.test/api/setup'])), 'HTTPS API')
    assert '<html' in output([*curl,'-H','Accept: text/html','https://acta.test/']).lower()
    assert 'push' in output([*curl,'https://acta.test/service-worker.js'])
    redirect = output(['curl','--noproxy','*','-sSI','-H','Host: acta.test',f'http://127.0.0.1:{http_port}/'])
    assert '308' in redirect and 'https://acta.test/' in redirect, redirect
    # Caddy must discard forged client headers and Acta must use Caddy's real
    # client address. A different forged value must not create a new bucket.
    request = [item for item in curl if item != '--fail']
    for i in range(11):
        code = output([*request,'-o','/dev/null','-w','%{http_code}',
                       '-H','Origin: https://acta.test','-H','Content-Type: application/json',
                       '-H',f'X-Forwarded-For: 203.0.113.{i+1}',
                       '--data','{"code":"wrong"}','https://acta.test/api/setup/unlock'])
        assert code == ('422' if i < 10 else '429'), code
    caddy_state = json.loads(output(['docker','inspect',caddy]))[0]
    caddy_ip = caddy_state['NetworkSettings']['Networks'][project+'_edge']['IPAddress']
    forbidden = [hashlib.sha256(('setup:'+ip).encode()).hexdigest()
                 for ip in [caddy_ip, *[f'203.0.113.{i+1}' for i in range(11)]]]
    for digest in forbidden:
        assert sql(f"SELECT count(*) FROM authentication_attempts WHERE bucket_hash=decode('{digest}','hex')") == '0'
    assert sql('SELECT count(*) FROM authentication_attempts WHERE attempts=11') == '1'
    # A request from inside the app container has an untrusted loopback peer.
    # Its bucket must remain separate from the external proxied requests.
    assert dc('exec','-T','app','curl','--silent','-o','/dev/null','-w','%{http_code}',
              '-H','Host: acta.test','-H','Origin: https://acta.test',
              '-H','Content-Type: application/json','-H','X-Forwarded-For: 203.0.113.88',
              '--data','{"code":"wrong"}','http://127.0.0.1:8081/api/setup/unlock') == '422'
    metadata = json.loads(output([*curl,'https://acta.test/.well-known/oauth-authorization-server']))
    assert metadata['issuer'] == 'https://acta.test', metadata
    assert sql("SELECT rolsuper::text || ':' || rolcreatedb::text || ':' || rolcreaterole::text FROM pg_roles WHERE rolname='acta'") == 'false:false:false'
    status = dc('exec','-T','app','cat','/proc/1/status')
    assert 'Uid:\t10001\t10001\t10001\t10001' in status, status
    assert 'CapEff:\t0000000000000000' in status, status
    for service in ['app','db']:
        state = json.loads(output(['docker','inspect',dc('ps','-q',service)]))[0]
        assert not state['HostConfig']['PortBindings'], state['HostConfig']['PortBindings']
        assert state['HostConfig']['Memory'] > 0
    key_before = dc('exec','-T','-u','10001','app','sha256sum','/run/acta/security-key')
    ca_before = hashlib.sha256((root/'root.crt').read_bytes()).hexdigest()
    sql('CREATE TABLE deployment_probe (value text); INSERT INTO deployment_probe VALUES (\'survives restart\')')
    run([*compose,'down'])  # Deliberately keep only this test project's volumes.
    run([*compose,'up','-d','--no-build','--wait','--wait-timeout','180'])
    assert sql('SELECT value FROM deployment_probe') == 'survives restart'
    assert dc('exec','-T','-u','10001','app','sha256sum','/run/acta/security-key') == key_before
    run(['docker','cp',dc('ps','-q','caddy')+':/data/caddy/pki/authorities/local/root.crt',str(root/'root.crt')],stdout=subprocess.DEVNULL)
    assert hashlib.sha256((root/'root.crt').read_bytes()).hexdigest() == ca_before
    if os.environ.get('COMPOSE_TEST_BACKUPS') == '1':
        operator = root/'config/backup'
        operator.mkdir(mode=0o700)
        repository = root/'repository'
        repository.mkdir(mode=0o700)
        with (root/'config/.env').open('a') as env:
            env.write(f'ACTA_BACKUP_REPOSITORY={repository}\n')
        age = output(['docker','run','--rm','--entrypoint','age-keygen','acta-backup-local:development'])
        (operator/'recovery.age').write_text(age+'\n')
        recipient = next(line.removeprefix('# public key: ') for line in age.splitlines() if line.startswith('# public key: '))
        config = json.loads((repo/'deploy/production/backup.example.json').read_text())
        config['recovery_recipient'] = recipient
        (operator/'backup.json').write_text(json.dumps(config))
        (operator/'database-url').write_text('postgres://postgres@/acta?host=/var/run/postgresql')
        (operator/'pgbackrest.conf').write_text((repo/'deploy/production/pgbackrest.example.conf').read_text().replace('REPLACE_WITH_A_RANDOM_SECRET',secrets.token_hex(32)))
        for path in operator.iterdir(): path.chmod(0o600)
        run([*compose,'--profile','backups','up','-d','--no-build','backup'])
        dc('exec','-T','backup','chown','999:10001','/repository')
        compose.extend(['-f',str(repo/'compose.backups.yaml'),'--profile','backups'])
        run([*compose,'up','-d','--no-build','--wait','--wait-timeout','180'])
        backrest = ['exec','-T','-u','999:10001','backup','pgbackrest',
                    '--config=/var/lib/acta-backup/config/pgbackrest.conf','--stanza=acta']
        dc(*backrest,'stanza-create')
        dc(*backrest,'check')
        worker = ['exec','-T','-u','999:10001','backup','acta-backup','--config','/var/lib/acta-backup/config/backup.json']
        state = json.loads(dc(*worker,'status'))
        assert state['configured'] and not state['policy']['enabled']
        # The application UID must be able to reach the shared socket with its private token.
        response = dc('exec','-T','-u','10001','app','sh','-c',
                      'curl --fail --silent --unix-socket /run/acta-backup/worker.sock -H "Authorization: Bearer $(cat /run/acta/backup-token)" http://localhost/v1/status')
        assert json.loads(response)['configured']
        for kind in ['full','drill']:
            job = json.loads(dc(*worker,kind))
            def finished():
                view = json.loads(dc(*worker,'status'))
                current = next(item for item in view['jobs'] if item['id'] == job['id'])
                if current['state'] == 'failed': raise AssertionError(current)
                return current['state'] == 'succeeded'
            wait(finished, kind+' backup job')
        run([*compose,'stop','app','db'])
        assert json.loads(dc(*worker,'status'))['configured']
        print('PASS: backup profile, WAL archiving, private app/worker socket, encrypted full backup, real restore drill and independent worker')
    print('PASS: HTTPS, redirect, forged-header rate limits, embedded UI, OAuth origin, least-privilege DB/app, private ports, limits, recreate persistence and TLS/key continuity')
finally:
    if os.environ.get('KEEP_COMPOSE_TEST') == '1':
        print(f'Retained {project}; configuration: {root}')
    else:
        subprocess.run([*compose,'down','--volumes','--remove-orphans'],cwd=repo,check=False)
        # The repository is owned by the test worker after exercising backups.
        if (root/'repository').exists():
            subprocess.run(['docker','run','--rm','--entrypoint','sh','-v',f'{root}/repository:/repository','acta-backup-local:development','-c',f'chown -R {os.getuid()}:{os.getgid()} /repository'],check=False)
        shutil.rmtree(root)
