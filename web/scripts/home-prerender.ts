import type { Plugin } from 'vite';
import content from '../src/home-content.json' with { type: 'json' };
import { canonicalHomeURL, homeStructuredData, homeTitle } from '../src/home-seo.ts';

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!);
}

function jsonData(value: unknown): string {
  // Keep a future content edit containing </script> from terminating JSON data.
  return JSON.stringify(value).replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/&/g, '\\u0026').replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
}

export function renderHomeHead(publicURL: string): string {
  const canonical = canonicalHomeURL(publicURL);
  const title = escapeHTML(homeTitle('D1 Apk'));
  const description = escapeHTML(content.description);
  return `<title>${title}</title>
    <meta name="description" content="${description}" />
    <meta name="robots" content="index,follow,max-image-preview:large" />
    <link rel="canonical" href="${escapeHTML(canonical)}" />
    <meta property="og:type" content="website" />
    <meta property="og:locale" content="zh_CN" />
    <meta property="og:site_name" content="D1 Apk" />
    <meta property="og:title" content="${title}" />
    <meta property="og:description" content="${description}" />
    <meta property="og:url" content="${escapeHTML(canonical)}" />
    <meta name="twitter:card" content="summary" />
    <meta name="twitter:title" content="${title}" />
    <meta name="twitter:description" content="${description}" />
    <script type="application/ld+json" id="home-structured-data">${jsonData(homeStructuredData('D1 Apk', canonical))}</script>`;
}

export function renderHomeBody(): string {
  const e = escapeHTML;
  return `<div class="home home-editorial home-prerender">
    <header class="home-header"><nav class="home-width home-nav" aria-label="首页导航"><a href="/">D1 Apk</a><div class="home-nav-actions"><a class="home-login" href="/#/login">登录</a><a class="home-button home-button-small" href="/#/register">免费开始</a></div></nav></header>
    <main>
      <section class="home-width home-hero"><div class="home-edition"><span>D1 / DIGITAL PUBLISHING</span><span>${e(content.hero.kicker)}</span></div><div class="home-hero-layout"><div class="home-hero-copy"><h1>${e(content.hero.lines[0])}<br /><span>${e(content.hero.lines[1])}</span></h1></div><div class="home-hero-aside"><p>${e(content.hero.description)}</p><div class="home-hero-actions"><a class="home-button" href="/#/register">免费创建账户</a></div></div></div></section>
      <section class="home-width home-solutions" id="home-solutions" aria-labelledby="home-solutions-heading"><div class="home-section-top"><div><div class="home-eyebrow">APP DISTRIBUTION SOLUTIONS</div><h2 id="home-solutions-heading">${e(content.solutions.heading)}</h2></div><p>${e(content.solutions.intro)}</p></div><div class="home-solution-grid">${content.solutions.items.map(item => `<article id="${e(item.id)}"${item.id === 'ads' ? ' class="home-solution-ads"' : ''}><div class="home-solution-top"><span>${e(item.label)}</span></div><h3>${e(item.title)}</h3><p>${e(item.description)}</p><ul class="home-solution-tags">${item.tags.map(tag => `<li>${e(tag)}</li>`).join('')}</ul></article>`).join('')}</div></section>
      <section class="home-width home-pwa-explainer"><div><h3>${e(content.paths.pwaHeading)}</h3><p>${e(content.paths.pwaDescription)}</p></div></section>
      <section class="home-width home-faq" id="home-faq" aria-labelledby="home-faq-heading"><div class="home-section-top"><div><div class="home-eyebrow">FREQUENTLY ASKED QUESTIONS</div><h2 id="home-faq-heading">${e(content.faq.heading)}</h2></div><p>${e(content.faq.intro)}</p></div><div class="home-faq-list">${content.faq.items.map((item, index) => `<details${index === 0 ? ' open' : ''}><summary>${e(item.question)}</summary><p>${e(item.answer)}</p></details>`).join('')}</div></section>
    </main><footer class="home-width home-footer"><a href="/">D1 Apk</a><span>APK 分发 · 网页转 PWA · Web Push</span><a href="/#/login">进入控制台</a></footer>
    <noscript><p class="home-width">应用控制台需要启用 JavaScript。你仍可在本页了解产品功能和常见问题。</p></noscript>
  </div>`;
}

export function homePrerender(publicURL: string): Plugin {
  canonicalHomeURL(publicURL);
  return {
    name: 'public-home-prerender',
    transformIndexHtml: {
      order: 'pre',
      handler: html => html.replace('<!-- HOME_SEO_HEAD -->', renderHomeHead(publicURL)).replace('<!-- HOME_PRERENDER -->', renderHomeBody()),
    },
  };
}
