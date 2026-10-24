import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import content from './home-content.json' with { type: 'json' };

declare const __SEO_PUBLIC_URL__: string;

export const DEFAULT_SEO_PUBLIC_URL = 'https://d1apk.com';
export const PRIVATE_PAGE_DESCRIPTION = '登录应用分发控制台，管理应用、下载入口与账户。';

// Accept a configured origin only. Invitation codes and request-controlled hosts
// must never become the canonical URL or structured-data identity.
export function canonicalHomeURL(value: string): string {
  const url = new URL(value);
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('SEO_PUBLIC_URL must be an HTTP(S) origin without a path, query or fragment.');
  }
  return `${url.origin}/`;
}

export function homeTitle(siteName: string): string {
  return `${siteName} · ${content.titleSuffix}`;
}

export function isPublicHomepage(hash: string): boolean {
  if (!hash || hash === '#') return true;
  return hash.replace(/^#/, '').split(/[?]/, 1)[0] === '/welcome';
}

export function homeStructuredData(siteName: string, canonical: string) {
  return {
    '@context': 'https://schema.org',
    '@graph': [
      { '@type': 'Organization', '@id': `${canonical}#organization`, name: siteName, url: canonical },
      { '@type': 'WebSite', '@id': `${canonical}#website`, name: siteName, url: canonical, inLanguage: 'zh-CN', publisher: { '@id': `${canonical}#organization` } },
      { '@type': 'SoftwareApplication', '@id': `${canonical}#software`, name: siteName, url: canonical, description: content.description, applicationCategory: 'DeveloperApplication', operatingSystem: 'Web', publisher: { '@id': `${canonical}#organization` } },
    ],
  };
}

function setMeta(attribute: 'name' | 'property', key: string, value: string) {
  let element = document.head.querySelector<HTMLMetaElement>(`meta[${attribute}="${key}"]`);
  if (!element) {
    element = document.createElement('meta');
    element.setAttribute(attribute, key);
    document.head.appendChild(element);
  }
  element.content = value;
}

export function applyPageMetadata(siteName: string, publicURL: string, hash = window.location.hash) {
  let canonical: string;
  try { canonical = canonicalHomeURL(publicURL); }
  catch { canonical = canonicalHomeURL(typeof __SEO_PUBLIC_URL__ === 'string' ? __SEO_PUBLIC_URL__ : DEFAULT_SEO_PUBLIC_URL); }
  const publicHome = isPublicHomepage(hash);
  const title = publicHome ? homeTitle(siteName) : `${siteName} · 应用分发控制台`;
  const description = publicHome ? content.description : PRIVATE_PAGE_DESCRIPTION;
  document.title = title;
  setMeta('name', 'description', description);
  setMeta('name', 'robots', publicHome ? 'index,follow,max-image-preview:large' : 'noindex,nofollow');
  setMeta('property', 'og:type', 'website');
  setMeta('property', 'og:locale', 'zh_CN');
  setMeta('property', 'og:site_name', siteName);
  setMeta('property', 'og:title', title);
  setMeta('property', 'og:description', description);
  setMeta('property', 'og:url', canonical);
  setMeta('name', 'twitter:card', 'summary');
  setMeta('name', 'twitter:title', title);
  setMeta('name', 'twitter:description', description);
  let link = document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]');
  if (!link) {
    link = document.createElement('link');
    link.rel = 'canonical';
    document.head.appendChild(link);
  }
  link.href = canonical;
  let structured = document.getElementById('home-structured-data');
  if (publicHome) {
    if (!structured) {
      structured = document.createElement('script');
      structured.id = 'home-structured-data';
      structured.setAttribute('type', 'application/ld+json');
      document.head.appendChild(structured);
    }
    structured.textContent = JSON.stringify(homeStructuredData(siteName, canonical));
  } else structured?.remove();
}

export function usePageMetadata(siteName?: string, publicURL?: string) {
  useEffect(() => {
    const update = () => applyPageMetadata(siteName || 'D1 Apk', publicURL || (typeof __SEO_PUBLIC_URL__ === 'string' ? __SEO_PUBLIC_URL__ : DEFAULT_SEO_PUBLIC_URL));
    update();
    window.addEventListener('hashchange', update);
    return () => window.removeEventListener('hashchange', update);
  }, [siteName, publicURL]);
}

// HashRouter uses history.pushState for Link navigation, which does not emit a
// native hashchange event. Observe its location in every public/auth/layout view.
export function RouteMetadata({ siteName, publicURL }: { siteName: string; publicURL: string }) {
  const location = useLocation();
  useEffect(() => {
    applyPageMetadata(siteName, publicURL, `#${location.pathname}${location.search}`);
  }, [siteName, publicURL, location.pathname, location.search]);
  return null;
}
