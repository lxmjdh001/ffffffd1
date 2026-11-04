#!/usr/bin/env python3
"""Stage P3TERX MMDB data; activate only after the application's own verifier passes."""
import argparse
import concurrent.futures
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import subprocess
import tempfile
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--directory', type=Path, default=Path('/var/lib/d1apk/geoip'))
    parser.add_argument('--validator', type=Path)
    parser.add_argument('--staged', type=Path, help='Reuse an earlier staged download')
    parser.add_argument('--activate', action='store_true')
    args = parser.parse_args()
    if args.activate and not args.validator:
        parser.error('--activate requires --validator pointing to a GeoIP-capable d1apk binary')
    os.umask(0o027)
    args.directory.mkdir(parents=True, exist_ok=True, mode=0o750)
    with (args.directory / '.update.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        stage = args.staged or Path(tempfile.mkdtemp(prefix='incoming-', dir=args.directory))
        files = ['GeoLite2-Country.mmdb', 'GeoLite2-ASN.mmdb']

        def download(name):
            path = stage / name
            url = 'https://github.com/P3TERX/GeoLite.mmdb/raw/download/' + name
            if not args.staged:
                request = urllib.request.Request(url, headers={'User-Agent': 'D1GeoIPDatabaseUpdater'})
                with urllib.request.urlopen(request, timeout=30) as response, path.open('xb') as output:
                    if not response.url.startswith('https://'):
                        raise ValueError('database download must remain HTTPS')
                    size = 0
                    while True:
                        block = response.read(1024 * 1024)
                        if not block:
                            break
                        size += len(block)
                        if size > 64 * 1024 * 1024:
                            raise ValueError('database exceeds size limit')
                        output.write(block)
            data = path.read_bytes()
            if not 1024 < len(data) <= 64 * 1024 * 1024 or b'\xab\xcd\xefMaxMind.com' not in data[-131072:]:
                raise ValueError('download is not an MMDB database')
            return {'name': name, 'url': url, 'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}

        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            reports = list(pool.map(download, files))
        report = {'stage': str(stage), 'files': reports, 'activated': False}
        if args.validator:
            checked = subprocess.run([str(args.validator), 'geoip-check', '-country', str(stage / files[0]), '-asn', str(stage / files[1])], check=True, capture_output=True, text=True, timeout=120)
            report['verification'] = json.loads(checked.stdout)
        if args.activate:
            user = pwd.getpwnam('d1apk')
            os.chown(args.directory, 0, user.pw_gid)
            os.chmod(args.directory, 0o750)
            stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
            for name in files:
                source, destination = stage / name, args.directory / name
                os.chown(source, 0, user.pw_gid)
                os.chmod(source, 0o640)
                if destination.exists():
                    os.link(destination, args.directory / (name + '.' + stamp + '.previous'))
                os.replace(source, destination)
            report['activated'] = True
            (args.directory / 'SOURCE.md').write_text('GeoLite2 data copyright MaxMind, Inc.\nSource: https://github.com/P3TERX/GeoLite.mmdb\nLicense: https://www.maxmind.com/en/geolite2/eula\nAttribution-ShareAlike: https://creativecommons.org/licenses/by-sa/4.0/\n')
        (stage / 'download-report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report))


if __name__ == '__main__':
    main()
