# Usage billing (migration 20)

No production deployment is implied by committing this change. Back up D1's
database before the separately authorized rollout. Apply migrations with the
new D1 binary; do not point an older binary at the migrated database.

All features use one zero-subscription plan. App and storage safety limits
remain, configurable in platform settings (defaults: 500 apps, 30 GiB).
Existing balances, holds, recharge orders and gift grants are preserved.
Subscription history remains available for audit, but new purchases return 410.
Previously issued download tickets retain their old transfer settlement rules.

Default USD rates, stored in integer micros and editable in Billing settings:

| Event | Rate | Registration credits |
| --- | ---: | --- |
| APK visit UV | 0.003 (preserves current operator setting) | Yes |
| PWA confirmed install | 0.03 | Yes |
| Cloak visit UV | 0.001 | No |
| Return-page display | 0.001 | No |
| Accepted push delivery | 0.001 | No |

APK and PWA share a single registration gift of 1,000 uses. Changing this
default affects new accounts only. No new monthly subscription gifts are minted.
Valid pre-existing grants are honored, expiring grants first.

UV is normalized IP plus user agent per owner/resource, rolling 24 hours,
independent of download hostname. Viewing details and downloading the same app
do not charge twice. APK charges on visit, not transfer completion.

PWA uses a reserved credit, completed only on the browser `appinstalled` event;
cancel releases it. Prompt acceptance alone is not an installation. Unsupported
browsers (notably manual iOS installs) cannot provide reliable confirmation and
are not billed by guessing. Reinstallation events are distinct; retries of the
same event are idempotent. This browser signal is not OS attestation.

Return pages reserve first, commit after DOM insertion, cancel if hidden before
display. Push reserves before delivery, commits on provider acceptance (not a
guarantee of display/read), and releases on terminal failure/cancellation.
Expired pending events are released by the existing maintenance worker.

Risk lookups require positive spendable balance or remaining valid gift credit.
Losing credit pauses fresh lookups but never re-enables known unsafe URLs.
Only configured targets are checked; the main site and payment configuration
are unaffected. Existing USDT reconciliation/settlement code is unchanged.
