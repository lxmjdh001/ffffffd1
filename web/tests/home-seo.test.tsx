import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { HashRouter, Link } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import content from '../src/home-content.json';
import { applyPageMetadata, canonicalHomeURL, homeStructuredData, homeTitle, isPublicHomepage, PRIVATE_PAGE_DESCRIPTION, RouteMetadata, usePageMetadata } from '../src/home-seo';
import { renderHomeBody, renderHomeHead } from '../scripts/home-prerender';

afterEach(() => { cleanup(); document.head.innerHTML = ''; window.history.replaceState(null, '', '/'); });

describe('public homepage metadata', () => {
  it('canonicalizes configured origins and rejects paths, credentials, query and fragment inputs', () => {
    expect(canonicalHomeURL('https://d1apk.com')).toBe('https://d1apk.com/');
    expect(canonicalHomeURL('https://EXAMPLE.com:443/')).toBe('https://example.com/');
    for (const value of ['javascript:alert(1)', 'ftp://example.com', 'https://user:password@example.com', 'https://example.com/apps', 'https://example.com/?invite=secret', 'https://example.com/#/billing']) {
      expect(() => canonicalHomeURL(value)).toThrow();
    }
  });

  it('treats the public welcome route as indexable and keeps private console routes out', () => {
    for (const hash of ['', '#', '#/welcome', '#/welcome?invite=private-code']) expect(isPublicHomepage(hash)).toBe(true);
    for (const hash of ['#/', '#/login', '#/register?invite=private-code', '#/apps', '#/billing', '#/account']) expect(isPublicHomepage(hash)).toBe(false);
  });

  it('keeps branded homepage metadata after configuration loads and strips invitation information from canonical data', () => {
    applyPageMetadata('星河分发', 'https://release.example.com', '#/welcome?invite=private-code');
    expect(document.title).toBe(homeTitle('星河分发'));
    expect(document.querySelector<HTMLMetaElement>('meta[name="description"]')?.content).toBe(content.description);
    expect(document.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href).toBe('https://release.example.com/');
    expect(document.querySelector<HTMLMetaElement>('meta[property="og:url"]')?.content).toBe('https://release.example.com/');
    expect(document.head.innerHTML).not.toContain('private-code');
    const data = JSON.parse(document.getElementById('home-structured-data')!.textContent!);
    expect(data['@graph'].map((entity: { name: string }) => entity.name)).toEqual(['星河分发', '星河分发', '星河分发']);
  });

  it('updates metadata on navigation, noindexes private pages, then restores homepage metadata', () => {
    function Metadata() { usePageMetadata('星河分发', 'https://release.example.com'); return null; }
    window.history.replaceState(null, '', '/#/welcome');
    render(<Metadata />);
    expect(document.title).toBe(homeTitle('星河分发'));
    window.history.replaceState(null, '', '/#/billing');
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    expect(document.title).toBe('星河分发 · 应用分发控制台');
    expect(document.querySelector<HTMLMetaElement>('meta[name="robots"]')?.content).toBe('noindex,nofollow');
    expect(document.getElementById('home-structured-data')).toBeNull();
    window.history.replaceState(null, '', '/#/welcome');
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    expect(document.querySelector<HTMLMetaElement>('meta[name="robots"]')?.content).toBe('index,follow,max-image-preview:large');
    expect(document.getElementById('home-structured-data')).not.toBeNull();
    expect(document.querySelectorAll('link[rel="canonical"]')).toHaveLength(1);
    expect(document.querySelectorAll('meta[name="description"]')).toHaveLength(1);
  });

  it('falls back to the build origin if runtime configuration contains a non-origin URL', () => {
    applyPageMetadata('D1 Apk', 'https://untrusted.example/?invite=secret', '#/welcome');
    expect(document.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href).toBe('https://d1apk.com/');
  });

  it('tracks real HashRouter Link navigation from homepage to login and back', async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, '', '/#/welcome?invite=private-code');
    render(<HashRouter><RouteMetadata siteName="星河分发" publicURL="https://release.example.com" /><Link to="/login">前往登录</Link><Link to="/welcome">返回首页</Link></HashRouter>);
    expect(document.title).toBe(homeTitle('星河分发'));
    expect(document.querySelector<HTMLMetaElement>('meta[name="description"]')?.content).toBe(content.description);
    await user.click(screen.getByRole('link', { name: '前往登录' }));
    await waitFor(() => expect(document.querySelector<HTMLMetaElement>('meta[name="robots"]')?.content).toBe('noindex,nofollow'));
    expect(document.title).toBe('星河分发 · 应用分发控制台');
    expect(document.querySelector<HTMLMetaElement>('meta[name="description"]')?.content).toBe(PRIVATE_PAGE_DESCRIPTION);
    expect(document.getElementById('home-structured-data')).toBeNull();
    await user.click(screen.getByRole('link', { name: '返回首页' }));
    await waitFor(() => expect(document.title).toBe(homeTitle('星河分发')));
    expect(document.querySelector<HTMLMetaElement>('meta[name="robots"]')?.content).toBe('index,follow,max-image-preview:large');
    expect(document.querySelector<HTMLMetaElement>('meta[name="description"]')?.content).toBe(content.description);
    expect(document.getElementById('home-structured-data')).not.toBeNull();
    expect(document.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href).toBe('https://release.example.com/');
    expect(document.head.innerHTML).not.toContain('private-code');
  });

  it('renders the same public product and FAQ content in the initial HTML, with data-only structured metadata', () => {
    const wrapper = document.createElement('div');
    wrapper.innerHTML = renderHomeBody();
    expect(wrapper.querySelectorAll('h1')).toHaveLength(1);
    expect(wrapper.querySelector('h1')?.textContent).toBe(content.hero.lines.join(''));
    for (const value of [content.paths.apkHeading, content.paths.apkDescription, content.paths.pwaTitle, content.paths.pwaSummary]) expect(wrapper.textContent).toContain(value);
    const features = homeStructuredData('D1 Apk', 'https://d1apk.com/')['@graph'][2].featureList!;
    for (const feature of features) expect(wrapper.textContent).toContain(feature);
    for (const item of content.faq.items) {
      expect(wrapper.textContent).toContain(item.question);
      expect(wrapper.textContent).toContain(item.answer);
    }
    expect(wrapper.querySelectorAll('[hidden], [aria-hidden="true"]')).toHaveLength(0);
    expect(wrapper.querySelectorAll('script')).toHaveLength(0);
    const head = renderHomeHead('https://d1apk.com');
    expect(head).toContain('type="application/ld+json"');
    expect(head).not.toContain('type="text/javascript"');
    expect(homeStructuredData('D1 Apk', 'https://d1apk.com/')['@graph']).toHaveLength(3);
  });
});
