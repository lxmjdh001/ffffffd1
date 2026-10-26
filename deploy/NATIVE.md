# Native deployment alongside existing sites

This installation uses the existing Debian Nginx and PostgreSQL. D1 has its own
system user, PostgreSQL database/role, systemd service, and data directory. Its
HTTP listener is `127.0.0.1:18088`; it does not bind public ports directly.

- Website: `https://d1apk.com`
- Website alias: `https://www.d1apk.com` redirects to the canonical website,
  preserving the request path and query. Run `deploy/www-redirect.py` as root
  after its DNS is pointed to the server. Its separate certificate renewal job
  is `/etc/cron.d/d1apk-www-renew`.
- Initial shared download domain: `https://test.d1apk.com`
- Customer CNAME target: `dns.d1apk.com` (DNS-only A to the deployment server)
- Secret environment: `/etc/d1apk/app.env`, root-only
- Release link: `/opt/d1apk/current`
- Data: `/var/lib/d1apk`
- Website Nginx config: `/etc/nginx/conf.d/d1apk.conf`
- Shared test domain: `/etc/nginx/conf.d/d1apk-public-test.conf`
- Customer configs: `/etc/nginx/conf.d/d1apk-customer-HOST.conf`

The bootstrap and activation scripts describe the initial deployment layout.
They are not general-purpose installers for arbitrary servers: inspect paths,
existing service names, and ports before reusing them. Do not upload a development
database or development environment file to production.

## Customer flow

1. Add an owned download subdomain in the domain management page.
2. Create a CNAME to `dns.d1apk.com` at the domain's DNS provider. DNS-only and
   proxied records are supported.
3. Click **检查解析并申请免费 SSL**. The application verifies the CNAME and
   enqueues certificate provisioning. If a proxy hides the CNAME, the application
   queues HTTP/ACME verification instead; the binding is not authorized yet.
4. The `d1apk-domains.timer` checks every minute. It reads verified and explicitly
   requested pending bindings from the dedicated D1 database. The adapted
   `sync-domain-nginx-certbot.sh` reuses the live PWA deployment's sequence:
   create HTTP vhost, run Certbot HTTP-01, then enable the HTTPS reverse proxy.
   It uses the same challenge webroot `/var/www/letsencrypt`, which the existing
   default HTTPS vhost can already serve for orange-cloud origin connections.
   D1 uses separate `d1apk-customer-*` configs, `d1apk-*` certificate names and
   backend port 18088. It does not edit PWA files or shared Nginx snippets.
   Nginx is validated before a graceful reload and existing site conflicts are
   rejected. Once TLS is available, proxy-backed domains must also return a
   random route marker before being marked verified. This check refreshes
   routing verification during renewal checks without repeatedly issuing certs.
   A generic 404 cannot by itself mark the binding ready. Outbound checks connect
   only to public IPs and reject cross-host redirects.
5. The page polls status and shows **SSL 已就绪** only after the HTTPS config has
   been installed. Failed issuance is retried after one hour; verifying again
   clears the retry delay. Port 80 must remain reachable for ACME validation.

Ready customer domains appear in application download links; the first ready
domain becomes the displayed traffic entry domain. Customer domains may serve
only their owner's applications and traffic pools. Registration and administration
remain on the main website. Main-site subdomains are reserved for the platform;
`CUSTOM_DOMAIN_TEST_HOSTS` permits exact operator-configured exceptions. This
deployment allows only `aaa.d1apk.com` and `bbb.d1apk.com` for integration tests.

The application periodically rechecks CNAMEs. Bindings older than 48 hours without
a successful verification stop being authorized. The helper removes only its own
customer config for deleted/expired bindings; certificate files remain available
for subsequent renewal/rebinding. No existing website configs are edited.

HTTP verification must remain reachable at `/.well-known/acme-challenge/`; proxy
WAF/challenge rules and forced HTTPS with incompatible origin TLS can prevent
verification. Failed checks show a retry/contact-support message. Ready
proxy-backed domains are revalidated every 12 hours; a failed response cannot
refresh their verification timestamp.

The live PWA application selects its `catch-all` provider, which skips the older
heuristic DNS/404 check and relies on the Nginx/Certbot provisioning flow. D1
therefore reuses that deployed flow, rather than copying the older content-match
heuristics from the local PWA source tree.

Ready customer certificates are checked every 12 hours and renewed when due.
Website and shared-test certificates have D1-specific renewal commands in
`/etc/cron.d/d1apk-cert-renew`. Nginx is reloaded after renewal.

## Operations

```sh
systemctl status d1apk d1apk-domains.timer
journalctl -u d1apk -n 100
journalctl -u d1apk-domains -n 100
systemctl start d1apk-domains
nginx -t
```

Application updates should be built into a separate release and health-checked
before changing the active release. Restart only `d1apk` for application updates;
do not restart PostgreSQL, Nginx or unrelated PM2 applications. Keep the database,
files, and `/etc/d1apk/app.env` backed up together.

After unpacking a release, make static artifacts readable to the service user
with `chmod -R a+rX RELEASE/web/dist`. After building as root, also use
`chmod 0755 RELEASE/bin RELEASE/bin/d1apk`; a restrictive build umask can otherwise
prevent the service account from executing the binary. Check access as `d1apk`
before restarting the running release. Do not apply these permissions to the secret environment
file. Verify both `/healthz` and the actual website `/` after every deployment.

### Template-library cover runtime

Administrator-created templates and later HTML/configuration edits enqueue real
preview screenshots automatically. The existing `d1apk` process runs the queue
for `SERVER_ROLE=all` or `control`; the `download` role does not render covers.
Keep the existing service and deployment layout.

Install Chromium and the fonts used by Chinese and emoji content on the native
Debian host, then validate using the service account:

```sh
sudo apt-get update
sudo apt-get install --no-install-recommends chromium fonts-noto-cjk fonts-noto-color-emoji
sudo -u d1apk /opt/d1apk/current/bin/d1apk check-landing-browser
```

These names are from Debian's official [Chromium package](https://packages.debian.org/stable/chromium),
[Noto CJK package](https://packages.debian.org/trixie/fonts-noto-cjk), and
[Noto Emoji package](https://packages.debian.org/trixie/fonts-noto-color-emoji).
The diagnostic renders an isolated test page without connecting to the database
or loading administrator credentials. It inherits only the optional screenshot
configuration, so pass the same `LANDING_SCREENSHOT_BROWSER` value when a custom
executable is configured in `/etc/d1apk/app.env`; do not print or source that
secret environment file merely to run this check. Leave
`LANDING_SCREENSHOT_NO_SANDBOX=false`.

Preserve `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem`, and `ProtectHome` in
the existing unit. The browser uses a fresh private temporary profile with its
own `HOME` and cache directories, and requires no access to `/home` or a desktop
session. `PrivateTmp` supplies its writable temporary directory. Validation on
the deployment host must also succeed under the service's existing restrictions;
do not disable service protections or the browser sandbox to bypass a failure.
This repository's local macOS check cannot certify a remote Debian host.

After the normal application update, confirm that an administrator can add a
template, see `cover_status` change from `pending`/`processing` to `ready`, and
see its real image in the library. Edit the HTML and save again: a changed page
must produce a new cover while a customer's existing favorite keeps its original
snapshot. This check must leave application visits, downloads, and billing
unchanged. Existing template/favorite covers are backfilled by migration v26;
failed jobs can be retried from template management after fixing browser setup.
See [DIY-LANDING.md](DIY-LANDING.md) for queue, image permissions, and configuration.

Local development continues through `start.sh` and `scripts/dev.sh`. On macOS,
install Google Chrome normally or set `LANDING_SCREENSHOT_BROWSER` to its
executable. The development script builds the application, runs the same real
screenshot check for control/all, and starts the server only after it succeeds.

### Administrator credentials

USDT payment setup and secondary-password wallet protection are documented in
[`USDT.md`](USDT.md). D1 only connects to the existing external UZF query service. Never deploy or
modify UZF here; configure only its query URL and HMAC secret in D1.

`ADMIN_ACCOUNT` accepts either an email address or a 3–32 character administrator
username (letters, numbers, hyphens and underscores). It takes precedence over
the legacy `ADMIN_EMAIL`; ordinary member registration still requires email.
Bootstrap variables do not overwrite an existing administrator on startup.

To rotate an existing administrator, run `bin/d1apk reset-admin` with the new
`ADMIN_ACCOUNT` and `ADMIN_PASSWORD` in a protected process environment. When
renaming, also set the operator-only `ADMIN_PREVIOUS_ACCOUNT` to the old login.
The command updates the same user record and revokes all its sessions in one
transaction. Persist the new credentials in the root-only environment file,
remove `ADMIN_PREVIOUS_ACCOUNT`, then restart only `d1apk`. Never commit passwords
or production environment files.
