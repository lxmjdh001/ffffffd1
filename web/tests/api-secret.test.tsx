import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AdsPage } from '../src/ads';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

const appConfig = {
  app_id: 'test-app', facebook_enabled: false, facebook_pixel: '', facebook_test_code: '', tiktok_enabled: false, tiktok_pixel: '', tiktok_test_code: '',
  open_facebook_event: 'ViewContent', register_facebook_event: 'CompleteRegistration', download_facebook_event: 'D1DownloadComplete',
  open_tiktok_event: 'ViewContent', register_tiktok_event: 'CompleteRegistration', download_tiktok_event: 'Download',
  allowed_origin: '', deep_link: '', allow_client_events: false, updated_at: '2026-10-07T00:00:00Z',
};
const savedKey = 'test-existing-signing-key';
const generatedKey = 'test-new-signing-key';
const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
let persistedKey: string;
let generations: number;
let client: QueryClient;
const dialogMethods = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};
beforeEach(() => {
  persistedKey = savedKey; generations = 0;
  vi.stubGlobal('scrollTo', vi.fn());
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === '/api/me') return json({ user: { id: 'test-member', role: 'member' }, csrf_token: 'test-csrf' });
    if (String(input).split('?')[0] === '/api/apps/test-app/ads') return json({ config: appConfig, facebook_token_set: false, tiktok_token_set: false, api_secret_set: !!persistedKey, api_secret: persistedKey, ...(String(input).endsWith('?include_tokens=1') ? {facebook_token:'fixture-meta-visible',tiktok_token:'fixture-tiktok-visible'} : {}) });
    if (String(input) === '/api/apps/test-app/ads/rotate-key') { generations++; persistedKey = generatedKey; return json({ api_secret: generatedKey }); }
    throw new Error(`Unexpected request: ${input}`);
  }));
});
afterEach(() => {
  cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks();
  for (const [name, descriptor] of Object.entries(dialogMethods)) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
  }
});
async function openPage() {
  const user = userEvent.setup();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<MemoryRouter initialEntries={['/apps/test-app/ads']}><AdminContext queryClient={client}><ConfigContext.Provider value={{ config: { public_url: 'http://localhost:8088' } as Config, reload: vi.fn() }}><Routes><Route path="/apps/:id/ads" element={<AdsPage />} /></Routes></ConfigContext.Provider></AdminContext></MemoryRouter>);
  await user.click(await screen.findByRole('button', { name: 'API 与 APK 接入' }));
  return user;
}
async function copyRepeatedly(user: ReturnType<typeof userEvent.setup>, key: string) {
  const write = vi.spyOn(navigator.clipboard, 'writeText').mockClear();
  for (let i = 1; i <= 3; i++) {
    await user.click(screen.getByRole('button', { name: '复制密钥' }));
    await waitFor(() => expect(write).toHaveBeenCalledTimes(i));
    expect(write).toHaveBeenLastCalledWith(key);
  }
}

it('shows a previously generated key and allows repeated copies after reopening with an empty query cache', async () => {
  let user = await openPage();
  expect(screen.getByText(savedKey)).toBeTruthy();
  await copyRepeatedly(user, savedKey);
  cleanup(); client.clear();
  user = await openPage();
  expect(screen.getByText(savedKey)).toBeTruthy();
  await copyRepeatedly(user, savedKey);
  expect(generations).toBe(0);
});

it('keeps a new key on the page after closing its dialog and after reloading', async () => {
  persistedKey = '';
  let user = await openPage();
  expect(screen.queryByRole('button', { name: '复制密钥' })).toBeNull();
  await user.click(screen.getByRole('button', { name: '生成 API 签名密钥' }));
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认生成' }));
  const dialog = within(screen.getByRole('dialog'));
  await dialog.findByText(generatedKey);
  expect(dialog.queryByText(/仅显示一次/)).toBeNull();
  await user.click(dialog.getByRole('button', { name: '完成' }));
  expect(screen.getByText(generatedKey)).toBeTruthy();
  await copyRepeatedly(user, generatedKey);
  cleanup(); client.clear();
  user = await openPage();
  expect(screen.getByText(generatedKey)).toBeTruthy();
  await copyRepeatedly(user, generatedKey);
  expect(generations).toBe(1);
});

it('restores both saved channel tokens as editable text in deep settings', async () => {
 const user=await openPage();
 await user.click(screen.getByRole('button',{name:'广告平台配置'}));
 await screen.findByDisplayValue('fixture-meta-visible');
 expect(screen.getByDisplayValue('fixture-meta-visible').getAttribute('type')).toBe('text');
 expect(screen.getByDisplayValue('fixture-tiktok-visible').getAttribute('type')).toBe('text');
});
