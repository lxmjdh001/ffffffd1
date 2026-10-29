import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { JSDOM } from 'jsdom';
import { afterEach, expect, it, vi } from 'vitest';

const parentSource = readFileSync(resolve(process.cwd(), '../internal/platform/templates/diy-parent.js'), 'utf8');
const goSource = readFileSync(resolve(process.cwd(), '../internal/platform/diy_landing_html.go'), 'utf8');
const childSource = /const diyChildJS = `([\s\S]*)`\s*$/.exec(goSource)?.[1] || '';
const documents: JSDOM[] = [];
afterEach(() => documents.splice(0).forEach(dom => dom.window.close()));

function parent(initiallyVisible = false) {
  const dom = new JSDOM(`<html><body data-channel="one-frame-channel" data-kind="pwa"><iframe id="d1-diy-content"></iframe><aside id="d1-diy-actions" ${initiallyVisible ? '' : 'hidden'}><button id="install-button">Install</button><span id="d1-diy-confirm" hidden>Continue</span><button id="d1-diy-cancel" hidden>Cancel</button></aside></body></html>`, { url: 'https://download.example/p/pwa-one/', pretendToBeVisual: true });
  documents.push(dom);
  const win = dom.window, document = win.document, frame = document.querySelector('iframe')!;
  const paid = vi.fn(), fetcher = vi.fn(), state = vi.spyOn(frame.contentWindow!, 'postMessage').mockImplementation(() => {});
  const button = document.getElementById('install-button') as HTMLButtonElement;
  button.addEventListener('click', paid);
  runInNewContext(parentSource, { window: win, document, navigator: win.navigator, MutationObserver: win.MutationObserver, Number, fetch: fetcher });
  const message = (value: Record<string, unknown>, source: unknown = frame.contentWindow) => win.dispatchEvent(new win.MessageEvent('message', { source: source as Window, data: { type: 'd1-diy', channel: 'one-frame-channel', ...value } }));
  return { win, document, frame, button, paid, fetcher, state, message, bar: document.getElementById('d1-diy-actions')!, cancel: document.getElementById('d1-diy-cancel') as HTMLButtonElement };
}

it('rejects forged sources, channels and unsupported actions without invoking any paid runtime', () => {
  const client = parent();
  client.message({ action: 'primary', request: 1 }, client.win);
  client.message({ channel: 'other-frame', action: 'primary', request: 2 });
  client.message({ action: 'complete', request: 3, installed: true, paid: true });
  client.message({ action: 'primary', request: 1.5 });
  client.message({ action: 'primary', request: 0 });
  expect(client.bar.hidden).toBe(true); expect(client.paid).not.toHaveBeenCalled(); expect(client.fetcher).not.toHaveBeenCalled();
});

it('only presents a trusted parent confirmation and preserves the original button runtime', () => {
  const client = parent();
  client.message({ action: 'primary', request: 1 });
  expect(client.bar.hidden).toBe(false); expect(client.cancel.hidden).toBe(false);
  expect(client.document.activeElement).toBe(client.button);
  expect(client.paid).not.toHaveBeenCalled(); expect(client.fetcher).not.toHaveBeenCalled();
  client.cancel.click(); expect(client.bar.hidden).toBe(true);
  client.message({ action: 'primary', request: 1 }); expect(client.bar.hidden).toBe(true);
  client.message({ action: 'primary', request: 2 }); expect(client.bar.hidden).toBe(false);
  client.button.click(); expect(client.paid).toHaveBeenCalledOnce(); expect(client.cancel.hidden).toBe(true);
});

it('synchronizes labels and disabled state without exposing business tokens to the child', async () => {
  const client = parent();
  client.message({ action: 'ready' });
  expect(client.state).toHaveBeenLastCalledWith({ type: 'd1-diy-state', channel: 'one-frame-channel', label: 'Install', disabled: false }, '*');
  client.button.textContent = 'Play'; client.button.disabled = true;
  await Promise.resolve();
  expect(client.state).toHaveBeenLastCalledWith({ type: 'd1-diy-state', channel: 'one-frame-channel', label: 'Play', disabled: true }, '*');
  expect(JSON.stringify(client.state.mock.calls)).not.toMatch(/token|price|complete|paid/);
});

it('keeps state synchronization active when child ready precedes its resource load', async () => {
  const client = parent(); client.message({ action: 'ready' });
  client.frame.dispatchEvent(new client.win.Event('load'));
  client.button.textContent = 'Play'; await Promise.resolve();
  expect(client.state.mock.calls.some(call => (call[0] as { label?: string }).label === 'Play')).toBe(true);
});

it('accepts a new child request sequence after an iframe reload', () => {
  const client = parent(); client.message({ action: 'primary', request: 4 }); client.cancel.click();
  client.frame.dispatchEvent(new client.win.Event('load')); client.message({ action: 'ready' });
  client.message({ action: 'primary', request: 1 });
  expect(client.bar.hidden).toBe(false); expect(client.paid).not.toHaveBeenCalled();
});

it('initiates a handshake even when the iframe finished loading before the parent runtime', () => {
  const client = parent();
  expect(client.state.mock.calls.some(call => (call[0] as { type?: string }).type === 'd1-diy-hello')).toBe(true);
});

function child(preview: boolean, body = '<button data-d1-action="primary"><span data-d1-bind="primary-label">Install</span></button><h1 data-d1-bind="app-name"></h1>') {
  const dom = new JSDOM(`<html><body>${body}</body></html>`, { url: 'https://download.example/p/pwa-one/diy-content', pretendToBeVisual: true });
  documents.push(dom);
  const win = dom.window, document = win.document, host = { postMessage: vi.fn() }, facade = { addEventListener: win.addEventListener.bind(win), history: win.history } as Record<string, any>;
  const config = { preview, channel: 'preview-channel', mode: 'pwa', label: 'Install', name: 'Example app', selector: '' };
  runInNewContext(childSource.replace('/*CONFIG*/', JSON.stringify(config)), { window: facade, document, parent: host, Object, String, URL });
  const message = (data: Record<string, unknown>, source: unknown = host) => win.dispatchEvent(new win.MessageEvent('message', { source: source as Window, data }));
  return { win, document, host, facade, message };
}

it('previews the primary action locally without requesting installation, download or billing', () => {
  expect(childSource).toContain('D1Landing'); const client = child(true);
  client.facade.D1Landing.primary();
  expect(client.document.getElementById('d1-diy-preview-status')?.textContent).toContain('预览：');
  expect(client.host.postMessage.mock.calls.some(call => call[0].action === 'primary')).toBe(false);
  expect(Object.isFrozen(client.facade.D1Landing)).toBe(true);
});

it('does not accept forged installed state and only applies state from the active parent channel', () => {
  const client = child(false), label = client.document.querySelector('[data-d1-bind="primary-label"]')!;
  client.message({ type: 'd1-diy-state', channel: 'preview-channel', label: 'Forged' }, {});
  client.message({ type: 'd1-diy-state', channel: 'other-channel', label: 'Forged' });
  client.message({ type: 'installed', channel: 'preview-channel', label: 'Forged' });
  expect(label.textContent).toBe('Install');
  client.message({ type: 'd1-diy-state', channel: 'preview-channel', label: 'Play', disabled: true });
  expect(label.textContent).toBe('Play'); expect((client.document.querySelector('button') as HTMLButtonElement).disabled).toBe(true);
});

it('keeps client history state local without adding joint browser entries or changing the content URL', () => {
  const client = child(false), history = client.win.history, url = client.win.location.href, length = history.length;
  history.pushState({ step: 1 }, '', '#checkout');
  expect(history.state).toEqual({ step: 1 }); expect(history.length).toBe(length); expect(client.win.location.href).toBe(url);
  history.replaceState({ step: 2 }, '', '/other-page'); history.back(); history.forward(); history.go(-1);
  expect(history.state).toEqual({ step: 2 }); expect(history.length).toBe(length); expect(client.win.location.href).toBe(url);
});

it('keeps customized button icons and input labels in sync with the bound PWA installation state', () => {
  const client = child(false, '<button data-d1-action="primary" data-d1-custom-label="立即体验" aria-label="立即体验"><svg aria-hidden="true"><path d="M0 0h10"/></svg><span data-d1-bind="primary-label">立即体验</span></button><input type="button" data-d1-action="primary" data-d1-bind="primary-label" data-d1-custom-label="立即体验" aria-label="立即体验" value="立即体验">');
  client.message({ type: 'd1-diy-state', channel: 'preview-channel', label: 'Play', disabled: false });
  expect(client.document.querySelector('button svg')).toBeTruthy();
  expect(client.document.querySelector('button span')?.textContent).toBe('Play');
  expect(client.document.querySelector('button')?.getAttribute('aria-label')).toBe('Play');
  const input = client.document.querySelector('input') as HTMLInputElement;
  expect(input.value).toBe('Play'); expect(input.getAttribute('aria-label')).toBe('Play');
  client.message({ type: 'd1-diy-state', channel: 'preview-channel', label: '立即体验', disabled: true });
  expect(input.value).toBe('立即体验'); expect(input.disabled).toBe(true);
  expect(client.document.querySelector('button svg')).toBeTruthy();
  expect(client.host.postMessage.mock.calls.some(call => ['primary', 'complete'].includes(call[0].action))).toBe(false);
});


it('ignores only optional Cloudflare beacon load failures while retaining real template diagnostics', () => {
  const client = child(true);
  const failure = (tag: string, src: string) => {
    const node = client.document.createElement(tag);
    node.setAttribute('src', src); client.document.body.append(node);
    node.dispatchEvent(new client.win.Event('error'));
  };
  for (const src of ['https://static.cloudflareinsights.com/beacon.min.js', 'https://static.cloudflareinsights.com/beacon.min.js/vce701823e78c4301a35b7e07294d8730']) failure('script', src);
  expect(client.host.postMessage.mock.calls.some(call => call[0].action === 'diagnostic')).toBe(false);
  for (const [tag, src] of [
    ['script', 'https://static.cloudflareinsights.com.evil.example/beacon.min.js'],
    ['script', 'https://static.cloudflareinsights.com/customer-app.js'],
    ['script', 'https://static.cloudflareinsights.com/beacon.min.js-not-a-beacon'],
    ['script', 'https://cdn.example/required.js'],
    ['img', 'https://static.cloudflareinsights.com/beacon.min.js'],
  ]) failure(tag, src);
  const diagnostics = () => client.host.postMessage.mock.calls.filter(call => call[0].action === 'diagnostic');
  expect(diagnostics()).toHaveLength(5);
  client.win.dispatchEvent(new client.win.ErrorEvent('error', { message: 'Customer script failed' }));
  expect(diagnostics()).toHaveLength(6);
  expect(diagnostics().at(-1)?.[0].message).toBe('Customer script failed');
});
