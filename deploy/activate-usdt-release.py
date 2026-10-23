"""Activate only D1, with automatic code/config rollback on failed health checks."""
import argparse
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import time
import urllib.request


def point_to(release):
    temporary = Path('/opt/d1apk') / ('.current-' + secrets.token_hex(8))
    temporary.symlink_to(release)
    os.replace(temporary, '/opt/d1apk/current')


def health():
    for _ in range(30):
        try:
            with urllib.request.urlopen('http://127.0.0.1:18088/healthz', timeout=2) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        time.sleep(1)
    raise RuntimeError('D1 health check failed')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--release', type=Path, required=True)
    parser.add_argument('--backup', type=Path, required=True)
    args = parser.parse_args()
    release = args.release.resolve()
    if os.geteuid() != 0 or release.parent != Path('/opt/d1apk/releases'):
        raise SystemExit('Expected root and a prepared D1 release')
    if args.backup.parent != Path('/opt/d1apk/backups'):
        raise SystemExit('Expected an explicit D1 backup directory')
    previous = Path((args.backup / 'previous-release').read_text().strip())
    if previous.parent != Path('/opt/d1apk/releases') or not (previous / 'bin/d1apk').is_file():
        raise SystemExit('Previous D1 release unavailable')
    if not (release / 'bin/d1apk').is_file() or not (args.backup / 'app.env').is_file():
        raise SystemExit('New binary or environment backup missing')
    try:
        point_to(release)
        subprocess.run(['systemctl', 'restart', 'd1apk'], check=True)
        health()
        req = urllib.request.Request('https://d1apk.com/?d1_release=usdt', headers={'User-Agent': 'Mozilla/5.0 D1DeploymentCheck'})
        with urllib.request.urlopen(req, timeout=30) as response:
            if response.status != 200:
                raise RuntimeError('Public homepage failed')
        print('D1 release active; database migrated; local health and public homepage passed')
    except Exception:
        shutil.copy2(args.backup / 'app.env', '/etc/d1apk/app.env')
        os.chmod('/etc/d1apk/app.env', 0o600)
        point_to(previous)
        subprocess.run(['systemctl', 'restart', 'd1apk'], check=True)
        health()
        raise SystemExit('Update failed; previous D1 code and configuration restored; database backup retained')


if __name__ == '__main__':
    main()
