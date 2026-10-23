# Native deployment alongside existing sites

This installation uses the existing Debian Nginx and PostgreSQL. D1 has its own
system user, PostgreSQL database/role, systemd service, and data directory. Its
HTTP listener is `127.0.0.1:18088`; it does not bind public ports directly.

- Website: `https://d1apk.com`
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
2. Create a DNS-only CNAME to `dns.d1apk.com` at the domain's DNS provider.
3. Click **检查解析并申请免费 SSL**. The application verifies the CNAME and
   enqueues certificate provisioning.
4. The `d1apk-domains.timer` checks every minute. The root helper reads verified
   bindings from the dedicated D1 database, creates only D1-prefixed Nginx
   configs, obtains a Let's Encrypt webroot certificate and validates Nginx
   before a graceful reload. Conflicts with existing site names are rejected.
5. The page polls status and shows **SSL 已就绪** only after the HTTPS config has
   been installed. Failed issuance is retried after one hour; verifying again
   clears the retry delay. Port 80 must remain reachable for ACME validation.

Ready customer domains appear in application download links; the first ready
domain becomes the displayed traffic entry domain. Customer domains may serve
only their owner's applications and traffic pools. Registration and administration
remain on the main website. Main-site subdomains are reserved for the platform.

The application periodically rechecks CNAMEs. Bindings older than 48 hours without
a successful verification stop being authorized. The helper removes only its own
customer config for deleted/expired bindings; certificate files remain available
for subsequent renewal/rebinding. No existing website configs are edited.

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
