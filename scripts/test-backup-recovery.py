#!/usr/bin/env python3
"""Real PostgreSQL/pgBackRest disaster drill, exclusively in a fresh container.
Requires Docker, Go, Python and an already built web bundle. No production paths,
network endpoints, credentials or databases are read. Use KEEP_BACKUP_TEST=1 to
retain the disposable container and /tmp artifacts after failure for inspection.
"""
from pathlib import Path
import json, os, shutil, subprocess, tempfile, uuid
repo = Path(__file__).resolve().parents[1]
root = Path(tempfile.mkdtemp(prefix='acta-backup-test-', dir='/tmp'))
name = 'acta-backup-test-' + uuid.uuid4().hex[:10]
env = dict(os.environ, CGO_ENABLED='0', TMPDIR='/tmp')
def run(args, **kwargs):
    subprocess.run(args, cwd=repo, env=env, check=True, **kwargs)
try:
    for package, target in [('build', 'acta2-server'), ('test', 'backup.test')]:
        args = ['go', package]
        if package == 'test': args += ['-c']
        args += ['-o', str(root/target), './cmd/acta2' if package=='build' else './internal/backup']
        run(args)
    run(['python3', 'scripts/seed-backup-test.py', str(root)])
    run(['docker', 'build', '-t', 'acta-backup-test', '-f', 'deploy/backup/test.Dockerfile', 'deploy/backup'])
    run(['docker', 'run', '-d', '--name', name, '--network', 'none', '-v', f'{root}:/review', 'acta-backup-test', 'sleep', 'infinity'])
    config = json.loads((root/'config.base.json').read_text())
    run(['docker', 'exec', name, 'chown', '-R', 'postgres:postgres', '/review'])
    run(['docker', 'exec', '-u', 'postgres', name, 'sh', '/review/setup.sh'])
    pub = subprocess.check_output(['docker','exec','-u','postgres',name,'age-keygen','-y','/review/recovery.age'], text=True).strip()
    config['recovery_recipient'] = pub
    # Write via stdin because the disposable test directory is now postgres-owned.
    run(['docker','exec','-i','-u','postgres',name,'sh','-c','umask 077; cat > /review/config.json'], input=json.dumps(config).encode())
    run(['docker','exec','-u','postgres','-e','ACTA_BACKUP_E2E_CONFIG=/review/config.json','-e','ACTA_BACKUP_E2E_REPOSITORY=/review/repository',name,'/review/backup.test','-test.run','TestPGBackRest','-test.v'])
finally:
    if os.environ.get('KEEP_BACKUP_TEST') == '1':
        print(f'Retained isolated container {name}; test files {root}')
    else:
        # Only the freshly named container and its own mounted scratch tree.
        subprocess.run(['docker','exec',name,'chown','-R',f'{os.getuid()}:{os.getgid()}','/review'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(['docker','rm','-f',name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        shutil.rmtree(root)
