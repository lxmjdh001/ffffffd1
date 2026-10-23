"""Back up D1 and configure its existing external UZF client. No UZF operations."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--patch', type=Path, required=True)
    parser.add_argument('--backup', type=Path, required=True)
    args = parser.parse_args()
    if os.geteuid() != 0 or args.backup.parent != Path('/opt/d1apk/backups') or args.backup.exists():
        raise SystemExit('Expected root and a new explicit D1 backup directory')
    updates = json.loads(args.patch.read_text())
    allowed = {'USDT_PROVIDER', 'USDT_MONITOR_URL', 'USDT_MONITOR_SECRET', 'USDT_MONITOR_ALLOW_HTTP', 'USDT_ADDRESS_PASSWORD_HASH'}
    if set(updates) != allowed or updates['USDT_PROVIDER'] != 'uzf':
        raise SystemExit('Only D1 USDT client settings may be changed')
    if any(not isinstance(value, str) or '\n' in value or '\r' in value for value in updates.values()):
        raise SystemExit('Invalid environment values')
    if not re.fullmatch(r'\$2[aby]\$12\$[./A-Za-z0-9]{53}', updates['USDT_ADDRESS_PASSWORD_HASH']):
        raise SystemExit('Expected a bcrypt cost-12 secondary password hash')
    if bool(updates['USDT_MONITOR_URL']) != bool(updates['USDT_MONITOR_SECRET']):
        raise SystemExit('The existing query URL and secret must be supplied together')
    if updates['USDT_MONITOR_ALLOW_HTTP'] not in {'true', 'false'}:
        raise SystemExit('Expected an explicit HTTP compatibility choice')
    env_path = Path('/etc/d1apk/app.env')
    original = env_path.read_text()
    args.backup.mkdir(mode=0o700, parents=True)
    shutil.copy2(env_path, args.backup / 'app.env')
    os.chmod(args.backup / 'app.env', 0o600)
    with (args.backup / 'database.dump').open('wb') as output:
        os.chmod(args.backup / 'database.dump', 0o600)
        subprocess.run(['runuser', '-u', 'postgres', '--', 'pg_dump', '-Fc', 'd1apk'], stdout=output, check=True)
    (args.backup / 'previous-release').write_text(str(Path('/opt/d1apk/current').resolve()) + '\n')
    pending = not updates['USDT_MONITOR_URL']
    lines = []
    for line in original.splitlines():
        key = line.split('=', 1)[0]
        lines.append(key + '=' + updates.pop(key) if key in updates else line)
    lines.extend(key + '=' + value for key, value in updates.items())
    fd, temporary = tempfile.mkstemp(prefix='.app.env-', dir=env_path.parent)
    try:
        with os.fdopen(fd, 'w') as handle:
            handle.write('\n'.join(lines) + '\n')
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, env_path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    args.patch.unlink()
    print('D1 database/config backed up; secondary password hash configured; existing URL/secret ' + ('pending, USDT unavailable' if pending else 'configured'))
    print('No UZF checkout, configuration, service, process or server was modified')


if __name__ == '__main__':
    main()
