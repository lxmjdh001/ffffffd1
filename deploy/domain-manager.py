#!/usr/bin/env python3
"""Provision only D1-managed Nginx vhosts; invoked by a root systemd timer."""
import datetime
import fcntl
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path('/var/lib/d1apk/acme')
CONF = Path('/etc/nginx/conf.d')
HOST = re.compile(r'^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$')

def run(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, **kwargs).stdout

def sql(query):
    return run(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-qAt', '-d', 'd1apk', '-v', 'ON_ERROR_STOP=1', '-c', query])

def quote(value):
    return "'" + value.replace("'", "''") + "'"

def config(host, tls=False):
    http = f'''server {{
    listen 80;
    server_name {host};
    location ^~ /.well-known/acme-challenge/ {{ root {ROOT}; }}
    location / {{ {'return 301 https://$host$request_uri;' if tls else 'return 503;'} }}
}}
'''
    if not tls:
        return http
    return http + f'''server {{
    listen 443 ssl http2;
    server_name {host};
    ssl_certificate /etc/letsencrypt/live/d1apk-{host}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/d1apk-{host}/privkey.pem;
    client_max_body_size 2048m;
    location ^~ /internal/ {{ return 404; }}
    location / {{
        proxy_pass http://127.0.0.1:18088;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_read_timeout 1800s;
        proxy_send_timeout 1800s;
    }}
}}
'''

def install(path, content):
    previous = path.read_text() if path.exists() else None
    if previous == content:
        return
    temp = path.with_suffix('.tmp')
    temp.write_text(content)
    os.replace(temp, path)
    try:
        run(['nginx', '-t'])
        run(['systemctl', 'reload', 'nginx'])
    except Exception:
        if previous is None:
            path.unlink(missing_ok=True)
        else:
            path.write_text(previous)
        raise

def main():
    ROOT.mkdir(parents=True, exist_ok=True)
    rows = json.loads(sql("SELECT coalesce(json_agg(row_to_json(d)), '[]') FROM (SELECT domains.id, hostname, ssl_status, ssl_retry_at FROM domains JOIN users ON users.id=domains.user_id WHERE verified_at > now()-interval '48 hours' AND NOT users.disabled) d"))
    allowed = {row['hostname'] for row in rows}
    for path in CONF.glob('d1apk-customer-*.conf'):
        host = path.name[len('d1apk-customer-'):-len('.conf')]
        if host not in allowed:
            # An absent/expired binding must stop serving, including port 80.
            path.unlink()
            run(['nginx','-t'])
            run(['systemctl','reload','nginx'])
    for row in rows:
        host, ident = row['hostname'], row['id']
        if not HOST.fullmatch(host) or '..' in host or not re.fullmatch(r'[a-f0-9]{32}', ident):
            continue
        retry = row['ssl_retry_at']
        if retry and datetime.datetime.fromisoformat(retry.replace('Z','+00:00')) > datetime.datetime.now(datetime.timezone.utc):
            continue
        path = CONF / f'd1apk-customer-{host}.conf'
        try:
            if host in {'d1apk.com','dns.d1apk.com','download.d1apk.com'}:
                raise ValueError('该域名为平台保留域名')
            if not path.exists():
                names = re.findall(r'\bserver_name\s+([^;]+);', run(['nginx','-T']))
                if any(host in line.split() for line in names):
                    raise ValueError('域名已用于服务器其他站点，请联系管理员')
                install(path, config(host))
            sql(f"UPDATE domains SET ssl_status='issuing', ssl_error='' WHERE id={quote(ident)}")
            run(['certbot','certonly','--webroot','-w',str(ROOT),'-d',host,'--cert-name',f'd1apk-{host}','--non-interactive','--agree-tos','--register-unsafely-without-email','--keep-until-expiring'], timeout=180)
            # Re-check the binding after the potentially slow certificate request.
            valid = sql(f"SELECT count(*) FROM domains JOIN users ON users.id=domains.user_id WHERE domains.id={quote(ident)} AND hostname={quote(host)} AND verified_at>now()-interval '48 hours' AND NOT users.disabled").strip()
            if valid != '1':
                continue
            install(path, config(host, True))
            run(['systemctl','reload','nginx'])
            sql(f"UPDATE domains SET ssl_status='ready', ssl_error='', ssl_retry_at=now()+interval '12 hours' WHERE id={quote(ident)}")
            print(f'{host}: SSL ready', flush=True)
        except Exception as exc:
            message = 'SSL 签发失败，请检查域名 CNAME、80 端口及 CAA 记录；系统稍后自动重试。'
            if isinstance(exc, ValueError):
                message = str(exc)
            sql(f"UPDATE domains SET ssl_status='error', ssl_error={quote(message)}, ssl_retry_at=now()+interval '1 hour' WHERE id={quote(ident)}")
            print(f'{host}: {message}', flush=True)

if __name__ == '__main__':
    with open('/run/d1apk-domains.lock','w') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise SystemExit(0)
        main()
