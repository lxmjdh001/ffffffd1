import { cleanup, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { IntegrationsPage } from '../src/ads';

let client: QueryClient;
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); });

it('shows each app’s reporting status independently of whether distribution is enabled', async () => {
  const apps = [
    { resource_kind:'apps',resource_id:'enabled',mode:'deep',facebook_pixel:'123456789',tiktok_pixel:'',facebook_enabled:true,tiktok_enabled:false,configured:true, name: '回传已开', slug: 'casino-01', platform: 'Android', version: '1.0', enabled:true },
    { resource_kind:'apps',resource_id:'disabled',mode:'deep',facebook_pixel:'123456780',tiktok_pixel:'',facebook_enabled:false,tiktok_enabled:false,configured:true, name: '回传未开', slug: 'casino-02', platform: 'Android', version: '2.0', enabled:false },
    { resource_kind:'apps',resource_id:'ios',mode:'deep',facebook_pixel:'123456781',tiktok_pixel:'',facebook_enabled:true,tiktok_enabled:false,configured:true, name: 'iOS 回传', slug: 'ios-casino', platform: 'iOS', version: '1.0', enabled:true },
  ];
  const fetcher = vi.fn(async () => new Response(JSON.stringify({ data: apps, total: apps.length })));
  vi.stubGlobal('fetch', fetcher);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MemoryRouter><IntegrationsPage /></MemoryRouter></QueryClientProvider>);
  await screen.findByRole('columnheader', { name: '回传状态' });
  const enabled = screen.getByText('回传已开').closest('tr')!;
  const disabled = screen.getByText('回传未开').closest('tr')!;
  expect(within(enabled).getByText('已开启').className).toContain('green');
  expect(within(disabled).getByText('未开启').className).toContain('gray');
  const headers = screen.getAllByRole('columnheader');
  const bindingIndex = headers.findIndex(header => header.textContent === '关联应用');
  expect(bindingIndex).toBeGreaterThan(-1);
  const enabledBinding = within(enabled).getAllByRole('cell')[bindingIndex];
  expect(within(enabledBinding).getByText('APK')).toBeTruthy();
  expect(within(enabledBinding).getByText('casino-01')).toBeTruthy();
  const disabledBinding = within(disabled).getAllByRole('cell')[bindingIndex];
  expect(within(disabledBinding).getByText('APK')).toBeTruthy();
  expect(within(disabledBinding).getByText('casino-02')).toBeTruthy();
  const iosBinding = within(screen.getByText('iOS 回传').closest('tr')!).getAllByRole('cell')[bindingIndex];
  expect(within(iosBinding).getByText('IPA')).toBeTruthy();
  expect(within(iosBinding).getByText('ios-casino')).toBeTruthy();
  expect(within(enabled).getByRole('link',{name:'日志'}).getAttribute('href')).toBe('/apps/enabled/ads?tab=events');
  // All statuses arrive with the paginated list, without one request per app.
  expect(fetcher).toHaveBeenCalledTimes(1);
});
