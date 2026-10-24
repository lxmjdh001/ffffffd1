# PWA landing templates

Source: `lxmjdh001/pwa`, branch `codex/pwa-lang-startup-fix`, commit `b575d0412e2740f3b420e56b8a8e765410382ce6`.

The original Google Play and custom image Vue templates, translations and assets are retained. Adaptations: asset prefix, scoped D1 config API for the custom template, D1-owned push subscription adapter, same-origin preview message checks, and CSP-compatible i18n JIT compilation. PWA installation, app scope and push delivery remain handled by D1. Build with `npm ci && npm run build` in this folder. Generated public assets are committed so Go embeds work on a clean checkout.
