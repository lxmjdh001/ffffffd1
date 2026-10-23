# D1 integration with the existing UZF service

Only D1 is deployed or changed. The operator already runs UZF on a dedicated
server. Do not modify UZF, deploy another copy, request OKX credentials, or reuse
go7 receiving addresses. go7 is a read-only reference for the client protocol.

## D1 configuration

- `USDT_PROVIDER=uzf` selects the existing signed query API for new orders.
- `USDT_MONITOR_URL` is its complete `/api/check` URL.
- `USDT_MONITOR_SECRET` is its existing HMAC query secret, not an OKX API secret.
- The go7 aliases `USDT_API_URL` and `USDT_API_SECRET` remain supported.
- Leave both URL and secret empty until the operator supplies them. The D1
  application still starts, but USDT is unavailable and new USDT orders are refused.
- Public production query endpoints require HTTPS by default. Only an operator
  who explicitly accepts the risk of unencrypted and unauthenticated responses
  may set `USDT_MONITOR_ALLOW_HTTP=true` for an existing UZF HTTP endpoint.
  HMAC authenticates the request, not the response; HTTPS is strongly preferred.
  This opt-in affects only the UZF monitor, never Futoon or TRON endpoints.
  No request credentials are forwarded across redirects. A literal loopback IP
  is accepted for legacy tests, not as a deployment default.

D1 uses the same sorted-parameter HMAC-SHA256 signature as go7. Query parameters
are exactly `amount`, `currency=USDT`, `timestamp`, and `signature`. It does not
send new wallet/time filters or require new UZF endpoints, snapshot freshness
fields, blockchain transaction hashes, network names, or deposit-address lists.

## Existing response and D1 settlement

D1 reads the original `success`, `data.found` and `data.transfer` response.
On a match, the original transfer fields `bill_id`, `amount`, `currency` and
`bill_timestamp` are checked locally. Numeric or string decimal amounts are
accepted without floating-point conversion. The bill must match the complete
order amount, USDT currency and original payment window. A bare `found=true`
without a bill is not credited. These checks require no UZF modification.

The globally unique `uzf:<bill_id>` settlement reference prevents the same bill
funding another order or user. Principal and the order-snapshotted recharge bonus
are credited atomically. Amount reservations are never reused for a receiving
wallet. The worker automatically checks orders each minute; users can also check
manually. The original 30-minute payment window and 48-hour reconciliation window
remain. This follows the trusted UZF account-bill model, not independent on-chain
confirmation. The configured receiving address must belong to the account
actually monitored by that existing service.

## Administrator address and secondary password

Use **平台配置 → USDT 收款配置**. Every address save, including clearing it to
disable new orders, requires the operator-specified secondary password.
`PUT /api/admin/usdt-wallet` enforces administrator authentication, CSRF and
five attempts per 15 minutes. General settings saves cannot bypass this check.

Only a bcrypt hash is stored in protected D1 environment configuration:
`USDT_ADDRESS_PASSWORD_HASH`. The password is never returned, sent to UZF, put
in frontend code, or included in audit logs. `cmd/hash-password` hashes standard
input; quote the resulting hash when sourcing a shell env file.

New orders snapshot the receiving address, amount, rate, bonus and provider.
Address changes never rewrite old orders. A null database wallet preserves the
legacy environment fallback; an explicit empty wallet disables new USDT orders.
Migration 8 is additive and preserves existing balances, orders and domains.

## Deployment and verification

Back up only the D1 database and `/etc/d1apk/app.env`, stage a new D1 release,
and restart only `d1apk`. Do not install UZF services or change its server.
`activate-usdt-release.py` supports D1-only code/config rollback on failed health
checks without automatically restoring a live financial database.

`go test ./...` covers the original UZF HTTP contract using D1-owned test doubles,
wallet-password enforcement, worker settlement, retries and duplicate bills.
No UZF process or source checkout is required. Real account verification remains
pending until the operator supplies the existing query URL and secret.
