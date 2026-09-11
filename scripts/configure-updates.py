#!/usr/bin/env python3
"""Provision the isolated updater for an existing deployment config. Starts nothing."""
import argparse, json, os, secrets
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--deployment',type=Path,required=True)
p.add_argument('--bundle',type=Path,required=True)
p.add_argument('--state',type=Path,required=True)
p.add_argument('--public-key',type=Path,required=True)
p.add_argument('--repository',default='peios/acta')
p.add_argument('--prereleases',action='store_true')
a=p.parse_args();os.umask(0o077)
root=a.deployment.resolve();bundle=a.bundle.resolve();state=a.state.resolve()
# All paths must have been provisioned by the operator, not supplied by a browser.
values=dict(line.split('=',1) for line in (root/'.env').read_text().splitlines() if line and not line.startswith('#'))
if (root/'update').exists() or (root/'update-token').exists():p.error('updater configuration already exists; refusing to replace it')
if state.exists():p.error('choose a new updater state directory')
for filename in ('compose.production.yaml','compose.backups.yaml','compose.updates.yaml'):
 if not (bundle/filename).is_file():p.error('bundle is missing '+filename)
state.mkdir(parents=True,mode=0o755);state.chmod(0o755)
(root/'update').mkdir();(root/'registry').mkdir(exist_ok=True)
(root/'update-token').write_text(secrets.token_hex(32)+'\n')
(root/'update/release.pub').write_bytes(a.public_key.read_bytes())
config=dict(repository=a.repository,public_key_file=str(root/'update/release.pub'),
 state_dir=str(state),socket='/run/acta-update/api.sock',token_file=str(root/'update-token'),
 project=values['ACTA_PROJECT'],installation=values['ACTA_INSTALLATION_ID'],
 compose_files=[str(bundle/f) for f in ('compose.production.yaml','compose.backups.yaml','compose.updates.yaml')],
 env_file=str(root/'.env'),backup_socket='/run/acta-backup/worker.sock',
 backup_token_file=str(root/'backup-token'),prereleases=a.prereleases,timeout_minutes=60,retain_recovery_copies=2)
(root/'update/config.json').write_text(json.dumps(config,indent=2)+'\n')
with (root/'.env').open('a') as f:f.write(f'ACTA_CHECKOUT_DIR={bundle}\nACTA_UPDATE_DIR={state}\n')
print('Updater configuration created. Bootstrap with a verified signed release before starting it.')
