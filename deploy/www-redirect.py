#!/usr/bin/env python3
"""Add the website's www alias without changing the canonical app origin."""
import importlib.util
from pathlib import Path
import re
import subprocess

def redirect_config(tls=False):
    content='''server {
    listen 80;
    server_name www.d1apk.com;
    location ^~ /.well-known/acme-challenge/ { root /var/www/letsencrypt; }
    location / { return 308 https://d1apk.com$request_uri; }
}
'''
    if tls:
        content+='''server {
    listen 443 ssl http2;
    server_name www.d1apk.com;
    ssl_certificate /etc/letsencrypt/live/d1apk-www.d1apk.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/d1apk-www.d1apk.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    location ^~ /.well-known/acme-challenge/ { root /var/www/letsencrypt; }
    location / { return 308 https://d1apk.com$request_uri; }
}
'''
    return content

def main():
    spec=importlib.util.spec_from_file_location('domains',Path(__file__).with_name('domain-manager.py'))
    domains=importlib.util.module_from_spec(spec);spec.loader.exec_module(domains)
    path=Path('/etc/nginx/conf.d/d1apk-www.conf')
    if not path.exists():
        names=re.findall(r'\bserver_name\s+([^;]+);',domains.run(['nginx','-T']))
        if any('www.d1apk.com' in line.split() for line in names):
            raise SystemExit('www.d1apk.com is already configured outside D1; refusing to overwrite')
    Path('/var/www/letsencrypt/.well-known/acme-challenge').mkdir(parents=True,exist_ok=True)
    has_cert=Path('/etc/letsencrypt/live/d1apk-www.d1apk.com/fullchain.pem').exists()
    domains.install(path,redirect_config(has_cert))
    subprocess.run(['certbot','certonly','--webroot','-w','/var/www/letsencrypt','-d','www.d1apk.com','--cert-name','d1apk-www.d1apk.com','--non-interactive','--agree-tos','--register-unsafely-without-email','--keep-until-expiring','--preferred-challenges','http'],check=True)
    domains.install(path,redirect_config(True))
    cron=Path('/etc/cron.d/d1apk-www-renew')
    cron.write_text('37 3 * * * root certbot renew --cert-name d1apk-www.d1apk.com --quiet --deploy-hook "nginx -t && systemctl reload nginx"\n')
    cron.chmod(0o644)
    print('www.d1apk.com HTTPS alias ready; redirects to https://d1apk.com')

if __name__=='__main__':main()
