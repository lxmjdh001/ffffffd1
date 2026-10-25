import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { JSDOM } from 'jsdom';
import { afterEach, expect, it, vi } from 'vitest';

const source = readFileSync(resolve(process.cwd(), '../internal/platform/templates/external-browser.js'), 'utf8');
const documents: JSDOM[] = [];
const android = 'Mozilla/5.0 (Linux; Android 14; wv) AppleWebKit/537.36 FB_IAB/FB4A';
const ios = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Instagram';
const url = 'https://shop.example/?campaign=1#/checkout';
function start(options: { ua?: string; config?: Record<string, unknown>; preview?: boolean; fallback?: boolean; zh?: boolean } = {}) {
 const dom = new JSDOM('<html><body><button id="install-button">Install</button><div id="app"><button class="install-button"><span>Install Vue</span></button></div><form id="d1-native-download"><button type="submit">Download</button></form><p id="install-status"></p></body></html>', { url: 'https://d1.example/p/abcdef/', pretendToBeVisual: true });
 documents.push(dom);
 const win = dom.window, document = win.document;
 document.documentElement.lang = options.zh ? 'zh-CN' : 'en';
 document.body.dataset.externalBrowser = JSON.stringify({ enabled: true, url, click_token:'signed-click-token', open_url: 'https://d1.example/p/abcdef/external/open', ...options.config });
 if (options.preview) document.body.classList.add('d1-apk-preview');
 let current = 'https://d1.example/p/abcdef/' + (options.fallback ? '?__d1_browser_fallback=1' : '');
 const navigations: string[] = [], timers = new Map<number, () => void>(); let next = 0;
 const location = { origin: 'https://d1.example', get href() { return current; }, set href(value: string) { navigations.push(value); current = value; } };
 const copy = vi.fn().mockResolvedValue(undefined), fetcher = vi.fn();
 const facade: Record<string, any> = { location, addEventListener: win.addEventListener.bind(win) };
 runInNewContext(source, { window: facade, document, navigator: { userAgent: options.ua || android, clipboard: { writeText: copy } }, URL, Date, fetch: fetcher, setTimeout: (callback: () => void) => { timers.set(++next, callback); return next; }, clearTimeout: (key: number) => timers.delete(key) });
 return { document, win, facade, copy, fetcher, navigations, timers, open: () => facade.D1ExternalBrowser.open(), expire: () => { const callbacks = [...timers.values()]; timers.clear(); callbacks.forEach(callback => callback()); } };
}
afterEach(() => { documents.splice(0).forEach(dom => dom.window.close()); });

it('only requests Android handoff on click and prevents the original Vue handler', () => {
 const c = start();
 expect(c.navigations).toEqual([]);
 const download = vi.fn(); c.document.querySelector('#app button')!.addEventListener('click', download);
 (c.document.querySelector('#app span') as HTMLElement).click();
 const intent = c.navigations[0];
 expect(intent).toMatch(/^intent:\/\/d1.example\/p\/abcdef\/external\/open\?key=[a-f0-9]{32}&token=signed-click-token#Intent;scheme=https;/);
 expect(intent).not.toContain('package=');
 expect(intent).toContain('S.browser_fallback_url=' + encodeURIComponent('https://d1.example/p/abcdef/?__d1_browser_fallback=1'));
 expect(download).not.toHaveBeenCalled(); expect(c.fetcher).toHaveBeenCalledOnce();
 expect(c.fetcher.mock.calls[0][1]).toMatchObject({method:'POST',keepalive:true});
 expect(c.document.getElementById('install-status')!.textContent).toBe('External opening requested.');
});

it('intercepts native APK submission before ticket creation and debounces duplicate requests', () => {
 const c = start(); const native = vi.fn(); const form = c.document.querySelector('form')!;
 form.addEventListener('submit', native);
 form.dispatchEvent(new c.win.Event('submit', { bubbles: true, cancelable: true }));
 c.open(); expect(c.navigations).toHaveLength(1); expect(native).not.toHaveBeenCalled();
});

it('requests iOS Safari with a relay so the final target hash is retained server-side', () => {
 const c = start({ ua: ios });
 expect(c.open()).toBe(true);
 expect(c.navigations[0]).toMatch(/^x-safari-https:\/\/d1.example\/p\/abcdef\/external\/open\?key=[a-f0-9]{32}&token=signed-click-token$/);
});

it('uses the configured URL in an ordinary browser', () => {
 const c = start({ ua: 'Mozilla/5.0 Android Chrome/130 Safari/537.36' });
 c.open(); expect(c.navigations[0]).toMatch(/^https:\/\/d1.example\/p\/abcdef\/external\/open\?key=[a-f0-9]{32}&token=signed-click-token$/); expect(c.timers.size).toBe(0);
});

it.each([{ enabled: false }, { continue: true }, { open_url: 'https://attacker.example/p/abcdef/external/open' }, { url: 'javascript:alert(1)' }])('keeps native behavior for inactive/invalid config %j', config => {
 const c = start({ config }); expect(c.open()).toBe(false); expect(c.navigations).toEqual([]);
});

it('does not launch in previews or relaunch an own landing in an ordinary browser', () => {
 expect(start({ preview: true }).open()).toBe(false);
 expect(start({ ua: 'Mozilla/5.0 Safari', config: { self_target: true } }).open()).toBe(false);
 const c = start({ config: { self_target: true } }); expect(c.open()).toBe(true);
});

it('offers safe manual fallback, copying and an Escape close without timed navigation', async () => {
 const c = start(); const button = c.document.getElementById('install-button')!; button.focus(); button.click(); c.expire();
 const dialog = c.document.querySelector('[role="dialog"]')!;
 expect(dialog).toBeTruthy(); expect(dialog.textContent).toContain('Your current app may block external opening.');
 expect(dialog.querySelector('a')!.href).toBe(url);
 (dialog.querySelector('.d1-external-tools button') as HTMLElement).click(); await Promise.resolve();
 expect(c.copy).toHaveBeenCalledExactlyOnceWith(url);
 expect(c.navigations).toHaveLength(1);
 dialog.dispatchEvent(new c.win.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
 expect(c.document.querySelector('[role="dialog"]')).toBeNull(); expect(c.document.activeElement).toBe(button);
});

it('cancels delayed fallback when the page hides after handoff', () => {
 const c = start(); c.open(); Object.defineProperty(c.document, 'hidden', { value: true });
 c.document.dispatchEvent(new c.win.Event('visibilitychange')); c.expire();
 expect(c.document.querySelector('[role="dialog"]')).toBeNull();
});

it('shows a manual guide after Android fallback without navigating again', () => {
 const c = start({ fallback: true, zh: true });
 expect(c.document.querySelector('[role="dialog"]')!.textContent).toContain('在外部浏览器中打开'); expect(c.navigations).toEqual([]);
 const bad = start({ fallback: true, config: { url: 'javascript:alert(1)' } }); expect(bad.document.querySelector('[role="dialog"]')).toBeNull();
});
