#!/usr/bin/env python3
"""Create a new, private production configuration. Never rotate existing secrets."""
import argparse
import base64
import os
from pathlib import Path
import re
import secrets
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--domain', required=True)
parser.add_argument('--email', required=True)
parser.add_argument('--directory', required=True, type=Path)
parser.add_argument('--project', default='acta2-production')
args = parser.parse_args()
if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?', args.domain) or '..' in args.domain:
    parser.error('domain must be a hostname without a scheme, path or port')
if not re.fullmatch(r'[A-Za-z0-9.!+_-]+@[A-Za-z0-9.-]+', args.email):
    parser.error('provide a certificate contact email')
if not re.fullmatch(r'[a-z0-9][a-z0-9_-]*', args.project):
    parser.error('project must use lowercase letters, numbers, underscores or hyphens')
root = args.directory.absolute()
if not re.fullmatch(r'/[A-Za-z0-9/_.-]+', str(root)):
    parser.error('directory must use letters, numbers, /, _, . or -')
os.umask(0o077)
root.mkdir(parents=True, exist_ok=False)
password = secrets.token_hex(32)
values = {
    'postgres-password': secrets.token_hex(32),
    'app-password': password,
    'database-url': f'postgres://acta:{password}@db:5432/acta2?sslmode=disable',
    'security-key': base64.b64encode(secrets.token_bytes(32)).decode().rstrip('='),
    'backup-token': secrets.token_hex(32),
    '.env': '\n'.join([
        f'ACTA_DOMAIN={args.domain}', f'ACME_EMAIL={args.email}',
        f'ACTA_DEPLOY_DIR={root}', f'ACTA_PROJECT={args.project}',
        f'ACTA_INSTALLATION_ID={uuid.uuid4()}', 'ACTA_VERSION=development', '',
    ]),
}
for name, content in values.items():
    (root/name).write_text(content + ('\n' if name != '.env' else ''))
print(f'Created private deployment configuration at {root}. No services started.')
