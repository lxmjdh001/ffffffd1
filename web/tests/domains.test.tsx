import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DomainsPage } from '../src/domains';
import { api, send } from '../src/api';
import { ConfigContext } from '../src/ui';
import type { Config, Domain } from '../src/types';

vi.mock('../src/api', () => ({ api: vi.fn(), send: vi.fn() }));
const clients: QueryClient[] = [];
const domain = { id: 'test-domain', hostname: 'download.example.com', remark: '', ssl_status: 'pending', verified_at: null } as Domain;
const dialogs = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};

beforeEach(() => {
  vi.mocked(api).mockResolvedValue({ data: [], total: 0 });
  vi.mocked(send).mockResolvedValue(domain);
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
});
afterEach(() => {
  cleanup(); clients.splice(0).forEach(client => client.clear()); vi.clearAllMocks();
  for (const [name, descriptor] of Object.entries(dialogs)) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
  }
});

function openDomains() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  const config = { cname_target: 'dns.example.com', domain_limit: 10, download_base_url: 'https://test.d1apk.com' } as Config;
  render(<AdminContext queryClient={client}><ConfigContext.Provider value={{ config, reload: vi.fn() }}><DomainsPage /></ConfigContext.Provider></AdminContext>);
  return userEvent.setup();
}

it('hides internal assigned-domain and main-site explanations without changing DNS guidance', async () => {
  openDomains();
  await screen.findByText('让下载链接拥有你的品牌');
  expect(screen.queryByText('系统分配的下载域名')).toBeNull();
  expect(screen.queryByText('https://test.d1apk.com')).toBeNull();
  expect(screen.queryByText(/官网仅用于登录和管理|官网与下载入口独立运行/)).toBeNull();
  expect(screen.getByText('dns.example.com')).toBeTruthy();
  expect(screen.getByRole('heading', { name: '我的域名' })).toBeTruthy();
  expect(vi.mocked(api).mock.calls.every(([path]) => String(path).startsWith('/domains?'))).toBe(true);
});

it('retains custom domain creation and the DNS/SSL setup dialog', async () => {
  const user = openDomains();
  await screen.findByText('让下载链接拥有你的品牌');
  await user.click(screen.getByRole('button', { name: '绑定域名', exact: true }));
  await user.type(screen.getByRole('textbox', { name: '下载域名', exact: true }), domain.hostname);
  await user.click(screen.getByRole('button', { name: '添加并查看解析' }));
  await screen.findByRole('heading', { name: '域名解析配置' });
  expect(send).toHaveBeenCalledWith('/domains', { hostname: domain.hostname, remark: '' });
  expect(screen.getByRole('button', { name: '检查解析并申请免费 SSL' })).toBeTruthy();
});
