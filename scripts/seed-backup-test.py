from pathlib import Path
import json,hashlib,base64,secrets
import sys
p=Path(sys.argv[1]);repo=Path(__file__).resolve().parents[1]
key=secrets.token_bytes(32)
(p/'security.key').write_text(base64.b64encode(key).decode().rstrip('='));(p/'security.key').chmod(0o600)
(p/'database-url').write_text('postgres://postgres@/acta?host=/review/source-socket');(p/'database-url').chmod(0o600)
(p/'api-token').write_text(secrets.token_hex(32));(p/'api-token').chmod(0o600)
(p/'release.json').write_text(json.dumps(dict(id='acta100-review',sha256=hashlib.sha256((p/'acta2-server').read_bytes()).hexdigest(),public_url='http://localhost:8081',installation='backup-isolated-review')))
(p/'pgbackrest.conf').write_text('[global]\nrepo1-path=/review/repository\nrepo1-cipher-type=aes-256-cbc\nrepo1-cipher-pass='+secrets.token_hex(32)+'\nlog-path=/review/log\nlock-path=/review/lock\nspool-path=/review/spool\nlog-level-file=detail\nrepo1-retention-full=2\nexpire-auto=n\n[acta]\npg1-path=/review/source\npg1-socket-path=/review/source-socket\npg1-port=5432\n')
(p/'pgbackrest.conf').chmod(0o600)
parts=['CREATE TABLE schema_migrations(name text PRIMARY KEY,checksum text NOT NULL);']
for file in sorted((repo/'internal/postgres/migrations').glob('*.sql')):
 text=file.read_text();parts += [text,"INSERT INTO schema_migrations VALUES ('migrations/"+file.name+"','"+hashlib.sha256(file.read_bytes()).hexdigest()+"');"]
parts += ["INSERT INTO security_key VALUES(true,decode('"+hashlib.sha256(key).hexdigest()+"','hex'));",'''INSERT INTO accounts(id,username) VALUES('10000000-0000-0000-0000-000000000001','backup-review');
INSERT INTO workspaces(id,name,slug) VALUES('10000000-0000-0000-0000-000000000002','Backup review','backup-review');
INSERT INTO tasks(id,workspace_id,number,title,status_id,created_by,created_at,updated_at) SELECT '10000000-0000-0000-0000-000000000003',workspace_id,1,'Recover this task',creation_status,'10000000-0000-0000-0000-000000000001',now(),now() FROM task_settings;
INSERT INTO documents VALUES('10000000-0000-0000-0000-000000000004','10000000-0000-0000-0000-000000000003',1);
INSERT INTO document_versions VALUES('10000000-0000-0000-0000-000000000005','10000000-0000-0000-0000-000000000004',1,'Recovery document','review.txt','text/plain',13,'HASH','10000000-0000-0000-0000-000000000001',now());
INSERT INTO document_files VALUES('10000000-0000-0000-0000-000000000005',convert_to('restore bytes','UTF8'));
CREATE TABLE recovery_probe(id int PRIMARY KEY);
INSERT INTO recovery_probe VALUES(1);
'''.replace('HASH',hashlib.sha256(b'restore bytes').hexdigest())]
(p/'seed.sql').write_text('\n'.join(parts))
(p/'setup.sh').write_text('''#!/bin/sh
set -eu
mkdir -p /review/source-socket /review/repository /review/log /review/lock /review/spool /review/state /review/restores
/usr/lib/postgresql/17/bin/initdb -D /review/source --auth=trust >/review/init.log
cat >> /review/source/postgresql.conf <<'EOF'
listen_addresses = ''
unix_socket_directories = '/review/source-socket'
archive_mode = on
archive_timeout = 60
archive_command = 'pgbackrest --config=/review/pgbackrest.conf --stanza=acta archive-push %p'
EOF
/usr/lib/postgresql/17/bin/pg_ctl -D /review/source -l /review/source.log -w start
createdb -h /review/source-socket acta
psql -X -v ON_ERROR_STOP=1 -h /review/source-socket -d acta -f /review/seed.sql >/review/seed.log
age-keygen -o /review/recovery.age 2>/review/age-public.log
pgbackrest --config=/review/pgbackrest.conf --stanza=acta stanza-create
pgbackrest --config=/review/pgbackrest.conf --stanza=acta check
''')
(p/'config.base.json').write_text(json.dumps(dict(release_dir='/review/releases',state_dir='/review/state',socket='/review/backup.sock',token_file='/review/api-token',database_url_file='/review/database-url',database_name='acta',database_user='postgres',stanza='acta',pgbackrest='/usr/bin/pgbackrest',age='/usr/bin/age',postgres_bin='/usr/lib/postgresql/17/bin',security_key_file='/review/security.key',release_file='/review/release.json',recovery_recipient='PLACEHOLDER',recovery_identity_file='/review/recovery.age',restore_root='/review/restores',job_timeout_minutes=10,min_retain_full=2,max_retain_full=30,destinations=[dict(id='review',name='Isolated encrypted repository',config_file='/review/pgbackrest.conf',repo=1)])))

with (p/'setup.sh').open('a') as f:
 f.write('digest=$(sha256sum /review/acta2-server | cut -d " " -f 1)\nmkdir -p /review/releases/$digest\ncp /review/acta2-server /review/releases/$digest/acta2-server\n')
