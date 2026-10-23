"""Prepare isolated UZF services and D1 config; never switch the D1 release."""
import argparse
import grp
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import subprocess
import tempfile


def atomic_write(path, contents, mode=0o600, gid=None):
    fd, temporary = tempfile.mkstemp(prefix='.' + path.name + '-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as handle:
            handle.write(contents)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, mode)
        if gid is not None:
            os.chown(temporary, 0, gid)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--release', type=Path, required=True)
    parser.add_argument('--secondary-hash-file', type=Path, required=True)
    parser.add_argument('--backup', type=Path, required=True)
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise SystemExit('Run as root')
    release = args.release.resolve()
    if release.parent != Path('/opt/uzf/releases') or not (release / 'venv/bin/python').is_file():
        raise SystemExit('Expected a prepared UZF release in /opt/uzf/releases')
    if args.backup.parent != Path('/opt/d1apk/backups') or args.backup.exists():
        raise SystemExit('Use a new, explicit D1 backup directory')
    hash_value = json.loads(args.secondary_hash_file.read_text())['secondary_hash']
    if not re.fullmatch(r'\$2[aby]\$12\$[./A-Za-z0-9]{53}', hash_value):
        raise SystemExit('Expected the operator-provided bcrypt cost-12 hash')
    try:
        user = pwd.getpwnam('d1uzf')
        if user.pw_dir != '/var/lib/d1uzf':
            raise SystemExit('Existing d1uzf user belongs to another deployment')
    except KeyError:
        subprocess.run(['useradd', '--system', '--home-dir', '/var/lib/d1uzf', '--shell', '/usr/sbin/nologin', 'd1uzf'], check=True)
        user = pwd.getpwnam('d1uzf')
    gid = grp.getgrnam('d1uzf').gr_gid
    data = Path('/var/lib/d1uzf')
    data.mkdir(mode=0o700, exist_ok=True)
    os.chown(data, user.pw_uid, gid)
    config_dir = Path('/etc/d1uzf')
    config_dir.mkdir(mode=0o750, exist_ok=True)
    os.chown(config_dir, 0, gid)
    os.chmod(config_dir, 0o750)
    config_path = config_dir / 'config.json'
    if config_path.exists():
        config = json.loads(config_path.read_text())
        api = config['query_api']
        if api.get('host') != '127.0.0.1' or api.get('port') != 16000 or len(api.get('secret', '')) < 32:
            raise SystemExit('Existing UZF settings conflict; not overwriting them')
    else:
        config = {'okx': {'api_base_url': 'https://openapi.okx.com', 'api_key': '', 'secret_key': '', 'passphrase': '', 'is_demo': False},
                  'monitor': {'interval': 10, 'json_file': '/var/lib/d1uzf/okx_transfers.json'},
                  'query_api': {'host': '127.0.0.1', 'port': 16000, 'secret': secrets.token_hex(32)}}
    env_path = Path('/etc/d1apk/app.env')
    original = env_path.read_text()
    args.backup.mkdir(mode=0o700, parents=True)
    shutil.copy2(env_path, args.backup / 'app.env')
    os.chmod(args.backup / 'app.env', 0o600)
    with (args.backup / 'database.dump').open('wb') as output:
        os.chmod(args.backup / 'database.dump', 0o600)
        subprocess.run(['runuser', '-u', 'postgres', '--', 'pg_dump', '-Fc', 'd1apk'], stdout=output, check=True)
    (args.backup / 'previous-release').write_text(str(Path('/opt/d1apk/current').resolve()) + '\n')
    atomic_write(config_path, json.dumps(config, indent=2) + '\n', 0o640, gid)
    updates = {'USDT_PROVIDER': 'uzf', 'USDT_MONITOR_URL': 'http://127.0.0.1:16000/api/check',
               'USDT_MONITOR_SECRET': config['query_api']['secret'], 'USDT_ADDRESS_PASSWORD_HASH': hash_value}
    lines = []
    for line in original.splitlines():
        key = line.split('=', 1)[0]
        lines.append(key + '=' + updates.pop(key) if key in updates else line)
    lines.extend(key + '=' + value for key, value in updates.items())
    atomic_write(env_path, '\n'.join(lines) + '\n')
    units = Path(__file__).resolve().parent
    for name in ['uzf-query.service', 'uzf-monitor.service']:
        atomic_write(Path('/etc/systemd/system') / name, (units / name).read_text(), 0o644)
    subprocess.run(['ln', '-sfn', str(release), '/opt/uzf/current'], check=True)
    subprocess.run(['systemctl', 'daemon-reload'], check=True)
    subprocess.run(['systemctl', 'enable', 'uzf-query.service'], check=True)
    subprocess.run(['systemctl', 'restart', 'uzf-query.service'], check=True)
    ready = all(config['okx'].get(key) and not config['okx'][key].startswith('your_') for key in ['api_key', 'secret_key', 'passphrase'])
    if ready:
        subprocess.run(['systemctl', 'enable', '--now', 'uzf-monitor.service'], check=True)
    else:
        print('New OKX credentials absent: monitor left stopped; payments remain unready')
    args.secondary_hash_file.unlink()
    print('UZF query configured; D1 backup saved; D1 release not switched yet')


if __name__ == '__main__':
    main()
