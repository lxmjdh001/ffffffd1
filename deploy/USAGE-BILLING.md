# Usage billing (migrations 20–21)

No production deployment is implied by committing this change. Back up D1's
database before the separately authorized rollout. Apply migrations with the
new D1 binary; do not point an older binary at the migrated database.

All features use one zero-subscription plan. App and storage safety limits
remain, configurable in platform settings (defaults: 500 apps, 30 GiB).
Existing cash balances and recharge orders are preserved; historical gift and
reservation records remain available for audit (cutover behavior below).
Subscription history remains available for audit, but new purchases return 410.
Previously issued download tickets retain their old transfer settlement rules.

Default USD rates, stored in integer micros. Only APK/PWA rates are editable
in Billing settings; the other three features stay free:

| Event | Rate | Registration credits |
| --- | ---: | --- |
| APK visit UV | 0.05 | Yes |
| PWA confirmed install | 0.05 | Yes |
| Cloak | Free | Not consumed |
| Return-page display | Free | Not consumed |
| Push notifications | Free | Not consumed |

New registrations receive **USD 100 in promotional usage credit**, not download
counts or withdrawable cash. The configurable gift is issued once, atomically
with registration, to a separate gift balance and ledger. It applies to APK/PWA
usage first; any shortfall uses cash balance. It never creates referral rewards
or withdrawal eligibility. Existing accounts are not automatically topped up.

Migration 21 retires unused test-era count grants (retaining reservations for
already-issued tickets), sets both new-order rates to USD 0.05, and releases all
pending cloak/return/push holds without charging them. Completed history and
in-flight APK/PWA quotes are preserved. Repeated migration does not reset
operator-edited prices or reissue gifts. Test accounts were confirmed disposable
by the operator; no conversion of old counts into cash is performed.

UV is normalized IP plus user agent per owner/resource, rolling 24 hours,
independent of download hostname. Viewing details and downloading the same app
do not charge twice. APK charges on visit, not transfer completion.

PWA uses a reserved credit, completed only on the browser `appinstalled` event;
cancel releases it. Prompt acceptance alone is not an installation. Unsupported
browsers (notably manual iOS installs) cannot provide reliable confirmation and
are not billed by guessing. Reinstallation events are distinct; retries of the
same event are idempotent. This browser signal is not OS attestation.

Return pages, cloaks and push have no balance requirement, gift consumption,
usage charge or billing reservation. Cached return-page clients can still call
the signed endpoint; it validates ownership and enabled state and permits the
display without billing. Historical free-feature reservations are release-only.
Expired pending APK/PWA events are released by the existing maintenance worker.

Risk lookups require positive spendable cash or promotional usage credit.
Losing credit pauses fresh lookups but never re-enables known unsafe URLs.
Only configured targets are checked; the main site and payment configuration
are unaffected. Existing USDT reconciliation/settlement code is unchanged.
