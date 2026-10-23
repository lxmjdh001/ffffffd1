#!/usr/bin/env python3
"""First deployment on Debian with an existing Nginx and PostgreSQL."""
import importlib.util
import os
from pathlib import Path
import secrets
import subprocess

def run(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, **kwargs).stdout

def db(query):
    return run(['runuser','-u','postgres','--','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-c',query])

release=Path('/opt/d1apk/releases/20261023-initial')
env=Path('/etc/d1apk/app.env')
if not env.exists():
    if db("SELECT count(*) FROM pg_roles WHERE rolname='d1apk'").strip()!='0' or db("SELECT count(*) FROM pg_database WHERE datname='d1apk'").strip()!='0':
        raise SystemExit('D1 database name already exists; refusing to overwrite')
    password=secrets.token_hex(24)
    db(f"CREATE ROLE d1apk LOGIN PASSWORD '{password}'")
    run(['runuser','-u','postgres','--','createdb','--owner=d1apk','d1apk'])
    key=Path('/etc/d1apk/google-key').read_text().strip()
    values={'APP_ENV':'production','APP_URL':'https://d1apk.com','HTTP_ADDR':'127.0.0.1:18088','DATA_DIR':'/var/lib/d1apk','WEB_DIR':'/opt/d1apk/current/web/dist','DATABASE_URL':f'postgres://d1apk:{password}@127.0.0.1:5432/d1apk?sslmode=disable','APP_SECRET':secrets.token_hex(32),'ADMIN_EMAIL':'admin@d1apk.com','ADMIN_PASSWORD':secrets.token_hex(12),'CNAME_TARGET':'dns.d1apk.com','SERVER_ROLE':'all','TRUSTED_PROXY_CIDRS':'127.0.0.1/32','CLAMAV_ADDR':'127.0.0.1:3310','GOOGLE_WEB_RISK_API_KEY':key}
    fd=os.open(env,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:
        f.write(''.join(f'{k}={v}\n' for k,v in values.items()))
if subprocess.run(['id','d1apk'],capture_output=True).returncode:
    run(['useradd','--system','--user-group','--home-dir','/var/lib/d1apk','--shell','/usr/sbin/nologin','d1apk'])
run(['chown','-R','d1apk:d1apk','/var/lib/d1apk'])
run(['chmod','755','/var/lib/d1apk','/var/lib/d1apk/acme'])
current=Path('/opt/d1apk/current')
if not current.exists():
    current.symlink_to(release,target_is_directory=True)
for name in ['d1apk.service','d1apk-domains.service','d1apk-domains.timer']:
    run(['install','-m','644',str(release/'deploy'/name),'/etc/systemd/system/'+name])
clam=Path('/etc/clamav/clamd.conf')
text=clam.read_text()
if 'TCPSocket 3310' not in text:
    clam.write_text(text+'\nTCPSocket 3310\nTCPAddr 127.0.0.1\nStreamMaxLength 2048M\nMaxScanSize 2048M\nMaxFileSize 2048M\n')
spec=importlib.util.spec_from_file_location('domains',release/'deploy/domain-manager.py')
domains=importlib.util.module_from_spec(spec);spec.loader.exec_module(domains)
conf=Path('/etc/nginx/conf.d/d1apk.conf')
if not conf.exists():
    domains.install(conf,domains.config('d1apk.com'))
run(['systemctl','daemon-reload'])
dropin=Path('/etc/systemd/system/clamav-daemon.socket.d')
dropin.mkdir(parents=True,exist_ok=True)
run(['install','-m','644',str(release/'deploy/clamav-d1apk.socket.conf'),str(dropin/'d1apk.conf')])
run(['systemctl','daemon-reload'])
run(['systemctl','enable','--now','clamav-freshclam'])
print('D1 isolated database, user, environment, Nginx HTTP entry and units prepared')
