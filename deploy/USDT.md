# USDT / UZF integration

The sibling `../uzf` checkout is an independent read-only OKX monitor and private
query API. Its `D1_INTEGRATION.md` documents local setup. It does not receive
customer orders or perform withdrawals; D1 owns orders, balances and settlement.

## Configuration

- `USDT_PROVIDER=uzf`: new orders use successful OKX deposit receipts. Default
  `tron` retains direct confirmed-chain verification. Existing orders snapshot
  their provider and are not switched by an environment change.
- `USDT_MONITOR_URL`: complete signed query URL, e.g.
  `http://127.0.0.1:16000/api/check` for co-located services. Public endpoints
  require HTTPS; production cleartext is permitted only for a literal loopback IP.
- `USDT_MONITOR_SECRET`: same private, 32+ character HMAC secret as UZF's
  `query_api.secret`; never put it in browser code or Git.
- `USDT_ADDRESS_PASSWORD_HASH`: bcrypt hash of the operator-specified secondary
  password. Generate with `go run ./cmd/hash-password` using standard input;
  do not pass passwords as command arguments. Single-quote the hash in an env
  file sourced by a shell, to preserve its `$` characters.

Wallet changes are in **平台配置 → USDT 收款配置**, not normal environment editing.
Every save, including disabling the channel with an empty address, requires the
secondary password. `PUT /api/admin/usdt-wallet` requires an administrator session,
CSRF token and secondary verification, rate-limited to five attempts / 15 minutes.
The general settings endpoint cannot change the wallet. The password and hash are
never returned in settings, included in audit events, or forwarded to UZF.

For upgrades, a nullable database wallet preserves the legacy `USDT_WALLET`
environment fallback until the first explicit administrator save. An explicit
empty address disables new USDT orders rather than falling back. Only newly
created orders use a changed address; previous instructions remain unchanged.
Migration 8 adds the wallet setting and order provider without changing balances,
existing order amounts or destinations. Back up the database and secret env file
together before production deployment.

## Native server layout

The production UZF checkout is isolated at `/opt/uzf/current`, with release-local
Python packages, user/group `d1uzf`, root-managed `/etc/d1uzf/config.json` (0640,
readable only by root and the service group), and data in `/var/lib/d1uzf`.
`uzf-query.service` listens only on `127.0.0.1:16000`. `uzf-monitor.service` is
installed but left disabled until the new OKX credentials are configured. No
public Nginx listener or PWA configuration needs changing.

`configure-uzf-native.py` validates explicit release/backup paths, generates a
production-only HMAC secret, backs up the D1 database and environment, and prepares
the two isolated units. Pass the operator's bcrypt hash using a protected JSON
file, never a plaintext command argument. `activate-usdt-release.py` switches and
restarts only D1, with code/config rollback if health checks fail. It never restores
the database automatically, which could discard payments made after the backup;
the additive migration remains compatible with the prior application release.

After filling the new account's read-only credentials in `/etc/d1uzf/config.json`,
run `systemctl enable --now uzf-monitor` and confirm the query `/health` reports
`payments_ready=true`. Fill the same account's TRC20 deposit address through the
administrator settings and verify a small real transfer before customer rollout.

## Settlement rules

D1 uses go7's sorted-parameter HMAC-SHA256 request signature. UZF receipts must
match the full six-decimal amount, receiving address, USDT/TRC20 network, deposit
success status, order time window and blockchain transaction ID. Stale snapshots,
missing credentials, unsigned/invalid queries and bare `found=true` are insufficient.
UZF also checks new-order addresses against the configured OKX account's deposit
addresses. Only read-permission credentials are needed; never supply a private key.

The worker checks pending/recently expired orders every minute, retaining a
48-hour reconciliation window. A delayed confirmation may settle a transfer that
was initiated within the order's original 30-minute payment window. The six-decimal
amount reservation is never reused for the same wallet. Both providers share the
same unique transaction namespace, so the same transfer cannot fund two orders.
Principal and any order-snapshotted recharge bonus are credited atomically.

Local tests exercise a real isolated Python service with temporary snapshots and
temporary databases:

```sh
UZF_TEST_PYTHON=/absolute/path/to/uzf/venv312/bin/python go test ./...
go vet ./...
npm --prefix web test -- --run
npm --prefix web run build
```

The running local UZF query API intentionally reports `payments_ready=false`
until the user's new OKX credentials are configured. No real transfer has been
performed. Do not treat fixture-based tests as proof of real account connectivity.
