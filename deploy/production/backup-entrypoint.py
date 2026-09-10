#!/usr/bin/env python3
"""Prepare private runtime copies, then run the independent worker as postgres."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys

os.umask(0o077)
base = Path('/var/lib/acta-backup')
config = base/'config'
config.mkdir(parents=True, exist_ok=True)
for source in Path('/run/operator').iterdir():
    if source.is_file():
        shutil.copyfile(source, config/source.name)
        os.chmod(config/source.name, 0o600)
for source, name in [('security-key', 'security.key'), ('backup-token', 'api-token')]:
    shutil.copyfile('/run/secrets/'+source, config/name)
    os.chmod(config/name, 0o600)
exe = Path('/usr/local/bin/acta2-server')
digest = hashlib.sha256(exe.read_bytes()).hexdigest()
release = Path('/releases')/digest
release.mkdir(parents=True, exist_ok=True)
if not (release/'acta2-server').exists():
    shutil.copyfile(exe, release/'acta2-server')
    os.chmod(release/'acta2-server', 0o755)
(config/'release.json').write_text(json.dumps({
    'id': os.environ['ACTA_RELEASE'], 'sha256': digest,
    'public_url': os.environ['ACTA_PUBLIC_URL'],
    'installation': os.environ['ACTA_INSTALLATION_ID'],
}))
# Only bootstrap directories and files we wrote; do not walk large restore trees.
for directory in [base, config, Path('/restore'), Path('/releases'), release]:
    directory.mkdir(parents=True, exist_ok=True)
    os.chown(directory, 999, 10001)
for target in [*config.iterdir(), release/'acta2-server']:
    os.chown(target, 999, 10001)
socket = Path('/run/acta-backup')
socket.mkdir(exist_ok=True)
os.chown(socket, 999, 10001)
os.chmod(socket, 0o750)
os.execvp('gosu', ['gosu', '999:10001', 'acta2-backup', '--config', str(config/'backup.json'), *sys.argv[1:]])
