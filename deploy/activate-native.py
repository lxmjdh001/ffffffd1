#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import subprocess
import time
import urllib.request

release=Path('/opt/d1apk/current')
def run(args):
    subprocess.run(args,check=True)

run(['systemctl','enable','--now','clamav-daemon'])
run(['systemctl','restart','clamav-daemon.socket','clamav-daemon.service'])
run(['systemctl','enable','--now','d1apk'])
for attempt in range(30):
    try:
        if urllib.request.urlopen('http://127.0.0.1:18088/healthz',timeout=2).status==200:
            break
    except Exception:
        time.sleep(1)
else:
    raise SystemExit('D1 health check failed; existing Nginx HTTPS configuration unchanged')
spec=importlib.util.spec_from_file_location('domains',release/'deploy/domain-manager.py')
domains=importlib.util.module_from_spec(spec);spec.loader.exec_module(domains)
domains.install(Path('/etc/nginx/conf.d/d1apk.conf'),domains.config('d1apk.com',True))
path=Path('/etc/nginx/conf.d/d1apk-public-test.conf')
if not path.exists():
    domains.install(path,domains.config('test.d1apk.com'))
run(['certbot','certonly','--webroot','-w','/var/lib/d1apk/acme','-d','test.d1apk.com','--cert-name','d1apk-test.d1apk.com','--non-interactive','--agree-tos','--register-unsafely-without-email','--keep-until-expiring'])
domains.install(path,domains.config('test.d1apk.com',True))
domains.sql("UPDATE settings SET download_base_url='https://test.d1apk.com', download_domain_mode='unified', download_domain_rules='[{\"url\":\"https://test.d1apk.com\",\"audience\":\"all\"}]' WHERE id=1")
# The platform administrator can replace the public test domain later.
Path('/etc/cron.d/d1apk-cert-renew').write_text('17 3 * * * root certbot renew --cert-name d1apk-d1apk.com --quiet --deploy-hook "nginx -t && systemctl reload nginx"\n27 3 * * * root certbot renew --cert-name d1apk-test.d1apk.com --quiet --deploy-hook "nginx -t && systemctl reload nginx"\n')
run(['systemctl','enable','--now','d1apk-domains.timer'])
print('D1 main site, public test downloads and domain certificate worker activated')
