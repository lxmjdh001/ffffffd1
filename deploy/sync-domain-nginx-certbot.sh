#!/usr/bin/env bash
# Adapted from the live PWA scripts/sync-domain-nginx-certbot.sh on this server.
# Uses the same webroot validation flow, but never edits PWA configs/snippets.
set -euo pipefail
DOMAIN="${1:-}"
if [[ ! "$DOMAIN" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]]; then
  echo 'invalid domain' >&2
  exit 2
fi
PROXY_TARGET="${DOMAIN_PROXY_TARGET:-127.0.0.1:18088}"
WEBROOT="${NGINX_CERTBOT_WEBROOT:-/var/www/letsencrypt}"
CONFIG_DIR="${NGINX_DOMAIN_CONFIG_DIR:-/etc/nginx/conf.d}"
CONFIG_FILE="${CONFIG_DIR}/d1apk-customer-${DOMAIN}.conf"
CERT_NAME="d1apk-${DOMAIN}"
CERT_DIR="/etc/letsencrypt/live/${CERT_NAME}"
mkdir -p "${WEBROOT}/.well-known/acme-challenge" "$CONFIG_DIR"
BACKUP=$(mktemp)
HAD_CONFIG=false
if [[ -f "$CONFIG_FILE" ]]; then
  cp "$CONFIG_FILE" "$BACKUP"
  HAD_CONFIG=true
fi
rollback() {
  if [[ "$HAD_CONFIG" == true ]]; then
    cp "$BACKUP" "$CONFIG_FILE"
  else
    rm -f -- "$CONFIG_FILE"
  fi
  nginx -t && systemctl reload nginx
}
trap 'rm -f -- "$BACKUP"' EXIT
write_proxy_locations() {
  cat <<EOF
    client_max_body_size 2048m;
    location ^~ /.well-known/acme-challenge/ {
        root ${WEBROOT};
        default_type text/plain;
        try_files \$uri =404;
    }
    location ^~ /internal/ { return 404; }
    location / {
        proxy_pass http://${PROXY_TARGET};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$remote_addr;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_read_timeout 1800s;
        proxy_send_timeout 1800s;
        add_header Cache-Control no-cache;
    }
EOF
}
write_config() {
  local use_tls="$1"
  local staged
  staged=$(mktemp "${CONFIG_FILE}.XXXXXX")
  {
    cat <<EOF
server {
    listen 80;
    server_name ${DOMAIN};
$(write_proxy_locations)
}
EOF
    if [[ "$use_tls" == true ]]; then
      cat <<EOF
server {
    listen 443 ssl http2;
    server_name ${DOMAIN};
    ssl_certificate ${CERT_DIR}/fullchain.pem;
    ssl_certificate_key ${CERT_DIR}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    error_page 497 https://\$host\$request_uri;
$(write_proxy_locations)
}
EOF
    fi
  } > "$staged"
  chmod 644 "$staged"
  mv "$staged" "$CONFIG_FILE"
  if ! nginx -t; then rollback; return 1; fi
  systemctl reload nginx
}
# Keep existing TLS available during renewal; initial issuance starts with HTTP.
if [[ -s "${CERT_DIR}/fullchain.pem" && -s "${CERT_DIR}/privkey.pem" ]]; then
  write_config true
else
  write_config false
fi
ACCOUNT_ARGS=(--register-unsafely-without-email)
if [[ -n "${ACME_EMAIL:-}" ]]; then ACCOUNT_ARGS=(--email "$ACME_EMAIL"); fi
certbot certonly \
  --webroot -w "$WEBROOT" \
  --cert-name "$CERT_NAME" -d "$DOMAIN" \
  --non-interactive --agree-tos "${ACCOUNT_ARGS[@]}" \
  --keep-until-expiring --preferred-challenges http
write_config true
echo "D1 nginx/certbot synced for ${DOMAIN}"
