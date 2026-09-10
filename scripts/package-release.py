#!/usr/bin/env python3
"""Build and push only the isolated repository's release images; emit a manifest."""
import argparse,json,os,re,subprocess
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--repository',required=True);p.add_argument('--version',required=True)
p.add_argument('--sequence',required=True,type=int);p.add_argument('--notes',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
a=p.parse_args()
if a.repository!='peios/acta2':p.error('release publishing remains restricted to peios/acta2 until the old Acta cutover is explicitly approved')
if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?',a.version):p.error('invalid version')
images={}
for service,target in [('app','app'),('db','database'),('backup','backup'),('updater','updater')]:
 tag=f'ghcr.io/{a.repository}-{service}:{a.version}'
 subprocess.run(['docker','buildx','build','--platform','linux/amd64','--target',target,'--build-arg',f'ACTA_VERSION={a.version}',
  '--label',f'org.opencontainers.image.source=https://github.com/{a.repository}',
  '--label',f'org.opencontainers.image.version={a.version}',
  '--tag',tag,'--push','.'],check=True)
 digest=json.loads(subprocess.check_output(['docker','buildx','imagetools','inspect',tag,'--format','{{json .Manifest.Digest}}'],text=True))
 images[service]=tag.split(':')[0]+'@'+digest
images['caddy']='caddy@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648'
schema=max(int(f.name.split('_')[0]) for f in Path('internal/postgres/migrations').glob('*.sql'))
a.output.write_text(json.dumps(dict(version=a.version,sequence=a.sequence,repository=a.repository,
 updater_protocol=1,deployment_layout=1,postgres_major=17,schema=schema,minimum_schema=40,
 notes=a.notes.read_text(),images=images),indent=2)+'\n')
