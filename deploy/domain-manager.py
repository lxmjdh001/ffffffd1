#!/usr/bin/env python3
"""Provision only D1-managed Nginx vhosts; invoked by a root systemd timer."""
import datetime
import fcntl
import json
import http.client
import ipaddress
import os
from pathlib import Path
import re
import secrets
import socket
import ssl
import subprocess
from urllib.parse import urljoin, urlsplit

ROOT = Path('/var/lib/d1apk/acme')
CUSTOMER_ROOT = Path(os.environ.get('NGINX_CERTBOT_WEBROOT', '/var/www/letsencrypt'))
CONF = Path('/etc/nginx/conf.d')
HOST = re.compile(r'^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$')

def run(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, **kwargs).stdout

def sql(query):
    return run(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-qAt', '-d', 'd1apk', '-v', 'ON_ERROR_STOP=1', '-c', query])

def quote(value):
    return "'" + value.replace("'", "''") + "'"

class PublicConnection(http.client.HTTPConnection):
    """Pin each connection to a public DNS result; never follow DNS to a LAN."""
    def connect(self):
        addresses = socket.getaddrinfo(self.host, self.port, type=socket.SOCK_STREAM)
        ips = list(dict.fromkeys(item[4][0] for item in addresses))
        if not ips or any(not ipaddress.ip_address(ip).is_global for ip in ips):
            raise ValueError('域名必须解析到公网地址，请检查 DNS 或联系客服')
        last = None
        for ip in ips:
            try:
                self.sock = socket.create_connection((ip, self.port), timeout=10)
                return
            except OSError as exc:
                last = exc
        raise last or OSError('domain connection failed')

class PublicTLSConnection(PublicConnection):
    def connect(self):
        super().connect()
        self.sock = ssl.create_default_context().wrap_socket(self.sock, server_hostname=self.host)

def verify_http(host, token, root=None):
    if not re.fullmatch(r'[a-f0-9]{48}', token):
        raise ValueError('域名验证标识无效，请联系客服')
    filename = 'd1apk-' + token + '-' + secrets.token_hex(8)
    directory = (root or ROOT) / '.well-known/acme-challenge'
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / filename
    expected = 'd1apk-route-' + secrets.token_urlsafe(32)
    path.write_text(expected)
    path.chmod(0o644)
    url = f'http://{host}/.well-known/acme-challenge/{filename}'
    try:
        for _ in range(4):
            parsed = urlsplit(url)
            if parsed.hostname != host or parsed.scheme not in {'http','https'} or parsed.username or parsed.password or parsed.port not in {None,80,443}:
                raise ValueError('域名跳转到了其他站点，请检查解析或联系客服')
            cls = PublicTLSConnection if parsed.scheme == 'https' else PublicConnection
            conn = cls(host, parsed.port or (443 if parsed.scheme == 'https' else 80), timeout=10)
            try:
                conn.request('GET', parsed.path + ('?' + parsed.query if parsed.query else ''), headers={'Host':host, 'User-Agent':'Mozilla/5.0 D1DomainVerification', 'Cache-Control':'no-cache'})
                response = conn.getresponse()
                body = response.read(1024).decode('utf-8',errors='replace')
                if response.status == 200 and body == expected:
                    return
                if response.status in {301,302,303,307,308} and response.getheader('Location'):
                    url = urljoin(url, response.getheader('Location'))
                    continue
                raise ValueError(f'HTTP 路由验证未通过（HTTP {response.status}），请检查 CNAME、代理或防火墙，也可联系客服')
            finally:
                conn.close()
        raise ValueError('域名重定向过多，请检查 SSL 设置或联系客服')
    finally:
        path.unlink(missing_ok=True)

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
    location ^~ /.well-known/acme-challenge/ {{ root {ROOT}; }}
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
    rows = json.loads(sql("SELECT coalesce(json_agg(row_to_json(d)), '[]') FROM (SELECT domains.id, hostname, token, verification_method, ssl_status, ssl_retry_at FROM domains JOIN users ON users.id=domains.user_id WHERE (verified_at > now()-interval '48 hours' OR (verification_method='http' AND verification_requested_at>now()-interval '24 hours')) AND NOT users.disabled) d"))
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
            sql(f"UPDATE domains SET ssl_status='issuing', ssl_error='' WHERE id={quote(ident)}")
            # Reuse the live PWA ordering and shared challenge webroot: create
            # HTTP vhost -> Certbot HTTP-01 -> install HTTPS vhost. In particular,
            # the existing default HTTPS vhost can serve the shared webroot when
            # Cloudflare uses HTTPS to the origin before this domain has a cert.
            run(['/bin/bash',str(Path(__file__).with_name('sync-domain-nginx-certbot.sh')),host], timeout=180)
            if row['verification_method'] == 'http':
                # A retained certificate alone does not establish current routing.
                # Confirm it again after the TLS vhost exists before refreshing
                # verified_at, including during periodic certificate checks.
                verify_http(host, row['token'], root=CUSTOMER_ROOT)
                print(f'{host}: public HTTP route verified', flush=True)
            # Re-check the binding after the potentially slow certificate request.
            valid = sql(f"SELECT count(*) FROM domains JOIN users ON users.id=domains.user_id WHERE domains.id={quote(ident)} AND token={quote(row['token'])} AND hostname={quote(host)} AND (verified_at>now()-interval '48 hours' OR (verification_method='http' AND verification_requested_at>now()-interval '24 hours')) AND NOT users.disabled").strip()
            if valid != '1':
                continue
            sql(f"UPDATE domains SET ssl_status='ready', ssl_error='', last_error='', verified_at=CASE WHEN verification_method='http' THEN now() ELSE verified_at END, ssl_retry_at=now()+interval '12 hours' WHERE id={quote(ident)} AND token={quote(row['token'])}")
            print(f'{host}: SSL ready', flush=True)
        except Exception as exc:
            message = '域名接入或 SSL 申请失败，请检查 CNAME、代理、80 端口及 CAA 记录，或联系客服；系统稍后重试。'
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
