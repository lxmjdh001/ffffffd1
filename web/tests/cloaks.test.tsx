import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CloaksPage, type Cloak, type CloakDraft } from '../src/cloaks';

const editableKeys = ['name', 'enabled', 'target_type', 'target_url', 'traffic_pool_slug', 'safe_url', 'blocked_languages', 'blocked_uas', 'blocked_countries', 'allowed_platforms', 'blocked_devices'].sort();
const baseDraft: CloakDraft = {
  name: '海外应用推广', enabled: true, target_type: 'url', target_url: 'https://download.example/app.apk', traffic_pool_slug: '', safe_url: 'https://brand.example/help',
  blocked_languages: [], blocked_uas: [], blocked_countries: [], allowed_platforms: [], blocked_devices: [],
};
const fixture = (overrides: Partial<Cloak> = {}): Cloak => ({
  ...structuredClone(baseDraft), id: '11111111111111111111111111111111', slug: 'a1b2c3d4', entry_url: 'https://d1apk.com/c/a1b2c3d4', created_at: '2026-10-24T04:00:00Z', updated_at: '2026-10-24T04:00:00Z', ...overrides,
});
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
type Mutation = { path: string; method: string; body?: CloakDraft };
let client: QueryClient;
let rows: Cloak[];
let mutations: Mutation[];
let nextSaveError: string | undefined;

beforeEach(() => {
  rows = [];
  mutations = [];
  nextSaveError = undefined;
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = String(input);
    const method = options?.method || 'GET';
    if (method === 'GET' && path === '/api/cloaks') return json({ data: rows, country_detection_configured: true });
    if (method === 'GET' && path === '/api/traffic-pools') return json({ data: [{ pool: { slug: 'launch-route', name: '应用下载分流', enabled: true }, entry_url: 'https://go.example/r/launch-route' }] });
    if ((method === 'POST' && path === '/api/cloaks') || (method === 'PUT' && path.startsWith('/api/cloaks/'))) {
      const body = JSON.parse(String(options?.body)) as CloakDraft;
      mutations.push({ path, method, body });
      // Match the backend's strict input schema: read-only response fields are rejected.
      if (Object.keys(body).some(key => !editableKeys.includes(key))) return json({ error: '请求包含不可编辑的字段' }, 400);
      if (nextSaveError) { const error = nextSaveError; nextSaveError = undefined; return json({ error }, 400); }
      const old = rows.find(row => path === `/api/cloaks/${row.id}`);
      const saved = fixture({ ...old, ...body, blocked_languages: body.blocked_languages.map(value => value.toLowerCase()) });
      rows = method === 'POST' ? [...rows, saved] : rows.map(row => row.id === saved.id ? saved : row);
      return json(saved);
    }
    if (method === 'DELETE' && path.startsWith('/api/cloaks/')) {
      mutations.push({ path, method });
      rows = rows.filter(row => path !== `/api/cloaks/${row.id}`);
      return json({ ok: true });
    }
    throw new Error(`Unexpected request: ${method} ${path}`);
  }));
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function (this: HTMLDialogElement) { this.removeAttribute('open'); } });
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

async function showPage() {
  render(<MemoryRouter initialEntries={['/cloaks']}><AdminContext queryClient={client}><CloaksPage /></AdminContext></MemoryRouter>);
  await screen.findByRole('heading', { name: '斗篷管理' });
  return userEvent.setup();
}
async function newEditor(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: '新建斗篷', exact: true }));
  await screen.findByRole('heading', { name: '新建斗篷' });
  await user.type(screen.getByRole('textbox', { name: '斗篷名称', exact: true }), '  海外应用推广  ');
  await user.type(screen.getByRole('textbox', { name: /安全页 URL/ }), 'https://brand.example/help');
}
async function chooseURL(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('radio', { name: /直接输入 URL/ }));
  await user.type(screen.getByRole('textbox', { name: '目标 URL', exact: true }), 'https://download.example/app.apk');
}
async function openTab(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(screen.getByRole('tab', { name: new RegExp(`^${name}`) }));
  return screen.getByRole('tabpanel');
}
async function saveNew(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: '保存并生成入口' }));
  await screen.findByText('斗篷已保存，入口可以使用了');
}

describe('cloak editor API integration', () => {
  it('creates a traffic target with independent language/country blocks, UA keywords, and multiple allowed platforms', async () => {
    const user = await showPage();
    await newEditor(user);
    await user.selectOptions(screen.getByRole('combobox', { name: '分流链接' }), 'launch-route');
    expect(screen.getAllByText('https://go.example/r/launch-route').length).toBeGreaterThan(0);
    await user.click(screen.getByRole('checkbox', { name: /中文（全部地区）/ }));
    await user.click(screen.getByRole('checkbox', { name: /^英语/ }));
    await openTab(user, 'UA 拦截');
    await user.type(screen.getByRole('textbox', { name: /UA 拦截关键词/ }), '  curl  \n\nHeadlessChrome\n');
    await openTab(user, '国家拦截');
    const search = screen.getByRole('textbox', { name: '搜索国家或地区' });
    await user.type(search, 'CN');
    await user.click(screen.getByRole('checkbox', { name: /^中国\s*CN$/ }));
    await user.clear(search); await user.type(search, '未知地区');
    await user.click(screen.getByRole('checkbox', { name: /未知地区/ }));
    expect(screen.getByLabelText('已选国家或地区')).toBeTruthy();
    await openTab(user, '平台允许');
    await user.click(screen.getByRole('checkbox', { name: /TikTok/ }));
    await user.click(screen.getByRole('checkbox', { name: /Facebook/ }));
    await saveNew(user);
    expect(mutations).toEqual([{ path: '/api/cloaks', method: 'POST', body: { ...baseDraft, target_type: 'traffic_pool', target_url: '', traffic_pool_slug: 'launch-route', blocked_languages: ['zh', 'en'], blocked_uas: ['curl', 'HeadlessChrome'], blocked_countries: ['CN', 'XX'], allowed_platforms: ['tiktok', 'facebook'] } }]);
  });

  it('clears the inactive target when switching between direct URL and traffic targets', async () => {
    const user = await showPage();
    await newEditor(user);
    await user.selectOptions(screen.getByRole('combobox', { name: '分流链接' }), 'launch-route');
    await chooseURL(user);
    await saveNew(user);
    expect(mutations[0].body).toEqual(baseDraft);
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    await user.click(screen.getByRole('radio', { name: /选择分流链接/ }));
    await user.selectOptions(screen.getByRole('combobox', { name: '分流链接' }), 'launch-route');
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[1].body).toEqual({ ...baseDraft, target_type: 'traffic_pool', target_url: '', traffic_pool_slug: 'launch-route' });
    expect(Object.keys(mutations[1].body!).sort()).toEqual(editableKeys);
  });

  it('shows normalized regional languages as checked and removes them without leaving a hidden rule', async () => {
    rows = [fixture({ blocked_languages: ['zh-cn', 'en'] })];
    const user = await showPage();
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    const simplifiedChinese = screen.getByRole('checkbox', { name: /简体中文（中国大陆）/ }) as HTMLInputElement;
    expect(simplifiedChinese.checked).toBe(true);
    expect((screen.getByRole('checkbox', { name: /^英语/ }) as HTMLInputElement).checked).toBe(true);
    await user.click(simplifiedChinese);
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[0].body?.blocked_languages).toEqual(['en']);
  });

  it('preserves all systems versus selected systems in the submitted device rules', async () => {
    const user = await showPage();
    await newEditor(user); await chooseURL(user);
    await openTab(user, '设备与系统');
    await user.click(screen.getByRole('checkbox', { name: '拦截电脑', exact: true }));
    const desktop = within(screen.getByRole('group', { name: '电脑系统拦截' }));
    expect((desktop.getByRole('checkbox', { name: '全部系统' }) as HTMLInputElement).checked).toBe(true);
    await user.click(desktop.getByRole('checkbox', { name: 'Windows', exact: true }));
    expect((desktop.getByRole('checkbox', { name: '全部系统' }) as HTMLInputElement).checked).toBe(false);
    const tablet = within(screen.getByRole('group', { name: '平板系统拦截' }));
    await user.click(tablet.getByRole('checkbox', { name: 'iOS / iPadOS', exact: true }));
    await user.click(screen.getByRole('checkbox', { name: '拦截手机', exact: true }));
    await saveNew(user);
    expect(mutations[0].body?.blocked_devices).toEqual([{ device: 'desktop', systems: ['macos', 'linux', 'chromeos', 'other'] }, { device: 'tablet', systems: ['ios'] }, { device: 'mobile', systems: ['all'] }]);
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    await openTab(user, '设备与系统');
    const restoredDesktop = within(screen.getByRole('group', { name: '电脑系统拦截' }));
    await user.click(restoredDesktop.getByRole('checkbox', { name: 'Windows', exact: true }));
    await user.click(screen.getByRole('checkbox', { name: '拦截平板', exact: true }));
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[1].body?.blocked_devices).toEqual([{ device: 'mobile', systems: ['all'] }, { device: 'desktop', systems: ['all'] }]);
  });

  it('turns interception off and back on using only editable fields while preserving all configured rules', async () => {
    rows = [fixture({ blocked_languages: ['zh-cn'], blocked_countries: ['US'], blocked_uas: ['curl'], allowed_platforms: ['google', 'bigo'], blocked_devices: [{ device: 'mobile', systems: ['android'] }] })];
    const user = await showPage();
    await user.click(screen.getByRole('button', { name: '关闭拦截' }));
    await screen.findByRole('button', { name: '开启拦截' });
    await user.click(screen.getByRole('button', { name: '开启拦截' }));
    await screen.findByRole('button', { name: '关闭拦截' });
    expect(mutations.map(item => ({ path: item.path, method: item.method, enabled: item.body?.enabled }))).toEqual([{ path: '/api/cloaks/11111111111111111111111111111111', method: 'PUT', enabled: false }, { path: '/api/cloaks/11111111111111111111111111111111', method: 'PUT', enabled: true }]);
    for (const { body } of mutations) {
      expect(Object.keys(body!).sort()).toEqual(editableKeys);
      expect(body).toMatchObject({ blocked_languages: ['zh-cn'], blocked_countries: ['US'], blocked_uas: ['curl'], allowed_platforms: ['google', 'bigo'], blocked_devices: [{ device: 'mobile', systems: ['android'] }] });
    }
  });

  it('requires deletion confirmation and removes the entry after the API succeeds', async () => {
    rows = [fixture()];
    const user = await showPage();
    await user.click(screen.getByRole('button', { name: '删除', exact: true }));
    expect(mutations).toHaveLength(0);
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(mutations).toHaveLength(0);
    await user.click(screen.getByRole('button', { name: '删除', exact: true }));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '删除斗篷' }));
    await screen.findByText('让每一次访问，都有合适的去向');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(mutations).toEqual([{ path: '/api/cloaks/11111111111111111111111111111111', method: 'DELETE' }]);
  });

  it('retains the complete editor after an API 400 and lets the user correct and resubmit', async () => {
    nextSaveError = '目标页和安全页不能填写本站斗篷入口';
    const user = await showPage();
    await newEditor(user); await chooseURL(user);
    await user.click(screen.getByRole('checkbox', { name: /^日语/ }));
    await user.click(screen.getByRole('checkbox', { name: /启用拦截规则/ }));
    await user.click(screen.getByRole('button', { name: '保存并生成入口' }));
    expect((await screen.findByRole('alert')).textContent).toContain('目标页和安全页不能填写本站斗篷入口');
    expect((screen.getByRole('textbox', { name: '斗篷名称', exact: true }) as HTMLInputElement).value).toBe('  海外应用推广  ');
    expect((screen.getByRole('textbox', { name: '目标 URL', exact: true }) as HTMLInputElement).value).toBe(baseDraft.target_url);
    expect((screen.getByRole('textbox', { name: /安全页 URL/ }) as HTMLInputElement).value).toBe(baseDraft.safe_url);
    expect((screen.getByRole('checkbox', { name: /^日语/ }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole('checkbox', { name: /启用拦截规则/ }) as HTMLInputElement).checked).toBe(false);
    const target = screen.getByRole('textbox', { name: '目标 URL', exact: true });
    await user.clear(target); await user.type(target, 'https://new.example/download');
    await saveNew(user);
    await waitFor(() => expect(mutations).toHaveLength(2));
    expect(mutations[1].body).toEqual({ ...baseDraft, enabled: false, target_url: 'https://new.example/download', blocked_languages: ['ja'] });
  });
});
