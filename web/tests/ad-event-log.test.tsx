import { cleanup, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { AdsPage } from '../src/ads';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('react-admin', () => ({ useNotify: () => vi.fn() }));
let client: QueryClient;
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); });

it.each(['apps', 'pwas'] as const)('shows the event and public app number in %s logs, with the correct app link', async kind => {
  const events = [
    { id: 'direct', event: kind === 'pwas' ? 'pwa_click' : 'download_start', application_slug: '9d30578e7f2b', verified_by: 'quick_download_server', event_id: 'private-deduplication-id', ip: '203.0.113.1', event_time: '2026-10-30T08:51:37Z', deliveries: [{ id:'delivery', platform:'facebook', status:'sent', attempts:1, event_name:'Lead' }] },
    { id: 'routed', event: 'download_start', application_slug: 'e8d0c8d2d045', downloaded_app_id: 'target-app', verified_by: 'quick_download_server', event_id: 'routed-deduplication-id', ip: '203.0.113.2', event_time: '2026-10-30T08:52:37Z', deliveries: [] },
    { id: 'deleted', event: 'download_complete', application_slug: '', downloaded_app_id: 'deleted-app', verified_by: 'download_server', event_id: 'deleted-deduplication-id', ip: '203.0.113.3', event_time: '2026-10-30T08:53:37Z', deliveries: [] },
  ];
  vi.stubGlobal('fetch', vi.fn(async (input: string) => new Response(JSON.stringify(input.includes('/events') ? {data:events,total:events.length} : {config:{mode:'quick'},facebook_token_set:true}))));
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/${kind}/source-app/ads?tab=events`]}><ConfigContext.Provider value={{config:{public_url:'https://example.test'} as Config,reload:vi.fn()}}><Routes><Route path={`/${kind}/:id/ads`} element={<AdsPage kind={kind}/>} /></Routes></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
  const direct = (await screen.findByRole('link',{name:'9d30578e7f2b'})).closest('td')!;
  expect(within(direct).getByText(kind === 'pwas' ? '点击安装' : '下载')).toBeTruthy();
  expect(within(direct).getByRole('link').getAttribute('href')).toBe(kind === 'pwas' ? '/pwas/source-app/edit' : '/apps/source-app');
  expect(screen.getByRole('link',{name:'e8d0c8d2d045'}).getAttribute('href')).toBe('/apps/target-app');
  expect(screen.getByRole('columnheader',{name:'事件 / 应用'})).toBeTruthy();
  expect(screen.queryByText('开始下载',{exact:true})).toBeNull();
  expect(screen.queryByText('查看实际下载应用')).toBeNull();
  expect(screen.queryByText(/deduplication-id/)).toBeNull();
  expect(screen.getByText('平台已接收')).toBeTruthy();
  const deleted = screen.getByText('203.0.113.3').closest('tr')!;
  expect(within(deleted).queryByRole('link')).toBeNull();
  expect(within(deleted).getByText('—')).toBeTruthy();
});
