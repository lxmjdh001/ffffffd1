import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { DashboardPage } from '../src/dashboard';
import { api } from '../src/api';
import { ConfigContext } from '../src/ui';
import type { Config, Dashboard } from '../src/types';

vi.mock('../src/api', () => ({ api: vi.fn() }));
vi.mock('react-admin', () => ({ useGetList: () => ({ data: [] }) }));
vi.mock('../src/download-domain', () => ({ useDownloadDomain: () => ({ data: null }) }));
let client: QueryClient;
afterEach(() => { cleanup(); client?.clear(); vi.clearAllMocks(); });

it('removes the gift-usage card and hint while preserving the cash balance and server app count', async () => {
  const dashboard = {
    user: { name: '测试用户', balance_micros: 12000000, held_micros: 2000000, gift_balance_micros: 100000000, gift_held_micros: 0 },
    plan: { id: 'metered', name: '按量计费', app_limit: 500, storage_bytes: 30 * 2 ** 30 },
    usage: { free_used: 0, paid_used: 0 }, app_count: 2, total_downloads: 0, active_domains: 0,
    storage_bytes: 0, quota_used: 0, quota_reserved: 0, quota_remaining: 0, quota_limit: 0,
    series: [],
  } as unknown as Dashboard;
  vi.mocked(api).mockResolvedValue(dashboard);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const config = { metered_billing: true, download_price_micros: 50000 } as Config;
  const { container } = render(<QueryClientProvider client={client}><MemoryRouter><ConfigContext.Provider value={{ config, reload: vi.fn() }}><DashboardPage /></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
  await screen.findByText('应用总数');
  expect(container.querySelectorAll('.dashboard-metric')).toHaveLength(3);
  expect(screen.queryByText('赠送用量额度')).toBeNull();
  expect(screen.queryByText('赠送额度优先抵扣 APK/PWA 用量，不可提现。')).toBeNull();
  const cards = [...container.querySelectorAll('.dashboard-metric')];
  expect(cards[0].querySelector('.dashboard-metric-value')?.textContent).toBe('2个');
  expect(cards[2].querySelector('.dashboard-metric-value')?.textContent).toBe('$ 10.000');
  expect(cards.every(card => !card.textContent?.match(/上限|累计分发|APK 访问 UV/))).toBe(true);
  expect(container.querySelector('.stat-footer')).toBeNull();
  expect(cards[0].querySelector('a')?.getAttribute('href')).toBe('/apps');
  expect(cards[1].querySelector('a')?.getAttribute('href')).toBe('/activity');
  expect(cards[2].querySelector('a')?.getAttribute('href')).toBe('/billing');
});
