import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CloaksPage, type Cloak, type CloakDraft } from '../src/cloaks';

const editableKeys = ['name', 'status', 'enabled', 'target_type', 'target_url', 'traffic_pool_slug', 'target_id', 'app_target_mode', 'safe_url', 'allowed_ips', 'blocked_ips', 'blocked_languages', 'block_empty_language', 'blocked_browsers', 'blocked_uas', 'blocked_countries', 'allowed_platforms', 'blocked_devices'].sort();
const baseDraft: CloakDraft = {
  name: '海外应用推广', status: 'rules', enabled: true, target_type: 'url', target_url: 'https://download.example/app.apk', traffic_pool_slug: '', target_id: '', app_target_mode: '', safe_url: 'https://brand.example/help',
  allowed_ips: [], blocked_ips: [], blocked_languages: [], block_empty_language: false, blocked_browsers: [], blocked_uas: [], blocked_countries: [], allowed_platforms: [], blocked_devices: [],
};
const appDraft: CloakDraft = { ...baseDraft, target_type: 'app', target_url: '', target_id: 'a'.repeat(32), app_target_mode: 'direct' };
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
    if (method === 'GET' && path === '/api/cloak-targets') return json({ apps: [{id:'a'.repeat(32),name:'测试 APK',direct_url:'https://go.example/a/demo/direct',detail_url:'https://go.example/a/demo/info'}, {id:'b'.repeat(32),name:'另一个 APK'}], pwas:[{id:'c'.repeat(32),name:'测试 PWA',entry_url:'https://d1apk.com/p/demo/'}] });
    if (method === 'GET' && path === '/api/download-domains') return json({data:[{url:'https://chosen.example',remark:'',source:'platform',audience:'all'}]});
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
async function newEditor(user: ReturnType<typeof userEvent.setup>, safeURL = 'https://brand.example/help') {
  await user.click(screen.getByRole('button', { name: '新建斗篷', exact: true }));
  await screen.findByRole('heading', { name: '新建斗篷' });
  await user.type(screen.getByRole('textbox', { name: '斗篷名称', exact: true }), '  海外应用推广  ');
  if (safeURL) await user.type(screen.getByRole('textbox', { name: /安全页 URL/ }), safeURL);
}
async function chooseURL(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('radio', { name: /应用链接/ }));
  await user.selectOptions(screen.getByRole('combobox', { name: '应用链接', exact: true }), 'a'.repeat(32));
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
  it('hides manual URL entry, creates app details and PWA targets, and preserves legacy URLs read-only', async () => {
    const user = await showPage();
    await newEditor(user);
    expect(screen.queryByRole('radio', { name: /直接输入 URL/ })).toBeNull();
    expect(screen.queryByRole('textbox', { name: '目标 URL', exact: true })).toBeNull();
    await chooseURL(user);
    await user.click(screen.getByRole('button', { name: '详情页', exact: true }));
    await saveNew(user);
    expect(mutations[0].body).toMatchObject({target_type:'app',target_id:'a'.repeat(32),app_target_mode:'info',target_url:'',traffic_pool_slug:''});
    await user.click(screen.getByRole('button', {name:'编辑',exact:true}));
    await user.click(screen.getByRole('radio', {name:/PWA 应用/}));
    await user.selectOptions(screen.getByRole('combobox', {name:'PWA 应用'}),'c'.repeat(32));
    await user.click(screen.getByRole('button',{name:'保存修改'}));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[1].body).toMatchObject({target_type:'pwa',target_id:'c'.repeat(32),app_target_mode:'',target_url:'',traffic_pool_slug:''});
  });

  it('opens domain selection instead of copying a fixed cloak domain', async () => {
    rows=[fixture()];
    const user=await showPage();
    await user.click(screen.getByRole('button',{name:'复制入口',exact:true}));
    const drawer=within(await screen.findByRole('dialog'));
    expect(drawer.getByRole('heading',{name:'选择域名复制链接'})).toBeTruthy();
    expect(await drawer.findByText('https://chosen.example/c/a1b2c3d4')).toBeTruthy();
    expect(screen.getByRole('dialog').className).toContain('side');
    expect(mutations).toHaveLength(0);
  });
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
    const platformPanel = await openTab(user, '广告允许');
    expect(platformPanel.textContent).toContain('选择允许的广告平台，可多选；不选则不限制。');
    expect(platformPanel.textContent).not.toMatch(/点击参数|链接参数|ttclid|fbclid|gclid|bbg/);
    expect(within(platformPanel).getByRole('group', { name: '允许广告平台' })).toBeTruthy();
    expect(screen.queryByRole('tab', { name: /^平台允许/ })).toBeNull();
    await user.click(screen.getByRole('checkbox', { name: /TikTok/ }));
    await user.click(screen.getByRole('checkbox', { name: /Facebook/ }));
    await saveNew(user);
    expect(mutations).toEqual([{ path: '/api/cloaks', method: 'POST', body: { ...baseDraft, target_type: 'traffic_pool', target_url: '', traffic_pool_slug: 'launch-route', blocked_languages: ['zh', 'en'], blocked_uas: ['curl', 'HeadlessChrome'], blocked_countries: ['CN', 'XX'], allowed_platforms: ['tiktok', 'facebook'] } }]);
  });

  it('creates and edits with an empty safe URL, preserving the default English 404 page', async () => {
    const user = await showPage();
    await newEditor(user, ''); await chooseURL(user);
    const safeInput = screen.getByRole('textbox', { name: /安全页 URL/ }) as HTMLInputElement;
    expect(safeInput.required).toBe(false);
    expect(safeInput.checkValidity()).toBe(true);
    expect(screen.queryByRole('region', { name: '默认安全页预览' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Account not found' })).toBeNull();
    expect(screen.queryByText('Please contact customer support for assistance.')).toBeNull();
    expect(screen.queryByText(/留空显示“Account not found”/)).toBeNull();
    const summary = within(screen.getByRole('complementary'));
    expect(summary.getByText('默认 404 页面')).toBeTruthy();
    expect(summary.getByText('Account not found')).toBeTruthy();
    await saveNew(user);
    expect(mutations[0]).toEqual({ path: '/api/cloaks', method: 'POST', body: { ...appDraft, safe_url: '' } });

    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    expect((screen.getByRole('textbox', { name: /安全页 URL/ }) as HTMLInputElement).value).toBe('');
    expect(within(screen.getByRole('complementary')).getByText('默认 404 页面')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[1]).toEqual({ path: '/api/cloaks/11111111111111111111111111111111', method: 'PUT', body: { ...appDraft, safe_url: '' } });
    expect(Object.keys(mutations[1].body!).sort()).toEqual(editableKeys);
  });

  it('creates, restores, and removes empty-language and browser blocks without affecting selected languages', async () => {
    const user = await showPage();
    await newEditor(user); await chooseURL(user);
    const emptyLanguage = screen.getByRole('switch', { name: '屏蔽空语言访问' }) as HTMLInputElement;
    expect(emptyLanguage.checked).toBe(false);
    expect(screen.getByText(/Accept-Language 缺失/).textContent).toContain('q > 0');
    await user.click(emptyLanguage);
    expect(within(screen.getByRole('complementary')).getByText('已设置 1 类规则')).toBeTruthy();
    await user.click(screen.getByRole('checkbox', { name: /^英语/ }));
    const browsers = within(await openTab(user, '客户端'));
    expect(browsers.getByText(/勾选的浏览器访问将进入安全页/)).toBeTruthy();
    for (const name of ['Chrome', 'Safari', 'Firefox', 'Edge']) {
      const browser = browsers.getByRole('checkbox', { name: new RegExp(`^${name}`, 'i') }) as HTMLInputElement;
      expect(browser.checked).toBe(false);
      await user.click(browser);
    }
    expect(within(screen.getByRole('complementary')).getByText('已设置 2 类规则')).toBeTruthy();
    await saveNew(user);
    expect(mutations[0].body).toEqual({ ...appDraft, blocked_languages: ['en'], block_empty_language: true, blocked_browsers: ['chrome', 'safari', 'firefox', 'edge'] });
    expect(screen.getByText('空语言')).toBeTruthy();
    expect(screen.getByText('客户端 4')).toBeTruthy();

    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    expect((screen.getByRole('switch', { name: '屏蔽空语言访问' }) as HTMLInputElement).checked).toBe(true);
    await user.click(screen.getByRole('switch', { name: '屏蔽空语言访问' }));
    expect((screen.getByRole('checkbox', { name: /^英语/ }) as HTMLInputElement).checked).toBe(true);
    const restoredBrowsers = within(await openTab(user, '客户端'));
    for (const name of ['Chrome', 'Safari', 'Firefox', 'Edge']) expect((restoredBrowsers.getByRole('checkbox', { name: new RegExp(`^${name}`, 'i') }) as HTMLInputElement).checked).toBe(true);
    await user.click(restoredBrowsers.getByRole('button', { name: '清空选择' }));
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[1].body).toEqual({ ...appDraft, blocked_languages: ['en'] });
    expect(screen.queryByText('空语言')).toBeNull();
    expect(screen.queryByText('客户端 4')).toBeNull();
  });

  it('creates multiline IP lists, restores them, and clears each list independently', async () => {
    const user = await showPage();
    await newEditor(user); await chooseURL(user);
    await openTab(user, 'IP 名单');
    const allowed = screen.getByRole('textbox', { name: 'IP 白名单', exact: true }) as HTMLTextAreaElement;
    const blocked = screen.getByRole('textbox', { name: 'IP 黑名单', exact: true }) as HTMLTextAreaElement;
    await user.type(allowed, '  192.0.2.1  \n\n2001:db8::1\n');
    expect(allowed.value).toBe('  192.0.2.1  \n\n2001:db8::1\n');
    await user.type(blocked, '198.51.100.7\n2001:db8::2');
    expect(screen.getByText(/同一 IP 同时在两份名单中时/)).toBeTruthy();
    await user.selectOptions(screen.getByRole('combobox', { name: '斗篷状态', exact: true }), 'off');
    expect(within(screen.getByRole('complementary')).getAllByText('跳过全部斗篷规则，包括 IP 黑名单，访问目标链接。').length).toBeGreaterThan(0);
    await saveNew(user);
    expect(mutations[0].body).toEqual({ ...appDraft, status: 'off', enabled: false, allowed_ips: ['192.0.2.1', '2001:db8::1'], blocked_ips: ['198.51.100.7', '2001:db8::2'] });
    expect(screen.getByText('白名单 2')).toBeTruthy();
    expect(screen.getByText('黑名单 2')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    await openTab(user, 'IP 名单');
    expect((screen.getByRole('textbox', { name: 'IP 白名单', exact: true }) as HTMLTextAreaElement).value).toBe('192.0.2.1\n2001:db8::1');
    expect((screen.getByRole('textbox', { name: 'IP 黑名单', exact: true }) as HTMLTextAreaElement).value).toBe('198.51.100.7\n2001:db8::2');
    await user.click(screen.getByRole('button', { name: '清空白名单' }));
    expect((screen.getByRole('textbox', { name: 'IP 黑名单', exact: true }) as HTMLTextAreaElement).value).toBe('198.51.100.7\n2001:db8::2');
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[1].body).toMatchObject({ allowed_ips: [], blocked_ips: ['198.51.100.7', '2001:db8::2'] });
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    await openTab(user, 'IP 名单');
    await user.click(screen.getByRole('button', { name: '清空黑名单' }));
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[2].body).toEqual({ ...appDraft, status: 'off', enabled: false });
    expect(screen.queryByText('白名单 2')).toBeNull();
    expect(screen.queryByText('黑名单 2')).toBeNull();
  });

  it('retains raw multiline IP input after validation fails so it can be corrected', async () => {
    const error = 'IP 黑名单无效：每行填写一个 IPv4 或 IPv6 地址，最多 1000 项';
    nextSaveError = error;
    const user = await showPage();
    await newEditor(user); await chooseURL(user);
    await openTab(user, 'IP 名单');
    const allowed = screen.getByRole('textbox', { name: 'IP 白名单', exact: true }) as HTMLTextAreaElement;
    const blocked = screen.getByRole('textbox', { name: 'IP 黑名单', exact: true }) as HTMLTextAreaElement;
    await user.type(allowed, '  192.0.2.1  \n\n');
    await user.type(blocked, 'invalid-ip\n');
    await user.click(screen.getByRole('button', { name: '保存并生成入口' }));
    expect((await screen.findByRole('alert')).textContent).toContain(error);
    expect(allowed.value).toBe('  192.0.2.1  \n\n');
    expect(blocked.value).toBe('invalid-ip\n');
    await user.clear(blocked);
    await user.type(blocked, '2001:db8::1');
    await saveNew(user);
    expect(mutations[1].body).toMatchObject({ allowed_ips: ['192.0.2.1'], blocked_ips: ['2001:db8::1'] });
  });

  it('treats absent rules from older cloak responses as disabled and persists their defaults', async () => {
    const legacy = fixture();
    Reflect.deleteProperty(legacy, 'block_empty_language');
    Reflect.deleteProperty(legacy, 'blocked_browsers');
    Reflect.deleteProperty(legacy, 'allowed_ips');
    Reflect.deleteProperty(legacy, 'blocked_ips');
    Reflect.deleteProperty(legacy, 'status');
    rows = [legacy];
    const user = await showPage();
    expect(screen.getByText('未设置条件')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    expect((screen.getByRole('switch', { name: '屏蔽空语言访问' }) as HTMLInputElement).checked).toBe(false);
    const browsers = within(await openTab(user, '客户端'));
    expect(browsers.getAllByRole('checkbox').every(input => !(input as HTMLInputElement).checked)).toBe(true);
    await openTab(user, 'IP 名单');
    expect((screen.getByRole('textbox', { name: 'IP 白名单', exact: true }) as HTMLTextAreaElement).value).toBe('');
    expect((screen.getByRole('textbox', { name: 'IP 黑名单', exact: true }) as HTMLTextAreaElement).value).toBe('');
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[0].body).toEqual(baseDraft);
  });

  it('previews custom safe URLs and lets an existing cloak switch back to the default 404 page', async () => {
    rows = [fixture()];
    const user = await showPage();
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    const summary = within(screen.getByRole('complementary'));
    expect(screen.queryByRole('textbox', {name:'目标 URL',exact:true})).toBeNull();
    expect(screen.getAllByText(baseDraft.target_url).length).toBeGreaterThan(0);
    expect(summary.getByText('自定义安全页')).toBeTruthy();
    expect(summary.getByText(baseDraft.safe_url)).toBeTruthy();
    expect(screen.queryByRole('region', { name: '默认安全页预览' })).toBeNull();
    await user.clear(screen.getByRole('textbox', { name: /安全页 URL/ }));
    expect(summary.getByText('默认 404 页面')).toBeTruthy();
    expect(summary.queryByText('自定义安全页')).toBeNull();
    expect(screen.queryByRole('region', { name: '默认安全页预览' })).toBeNull();
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[0]).toEqual({ path: '/api/cloaks/11111111111111111111111111111111', method: 'PUT', body: { ...baseDraft, safe_url: '' } });
  });

  it('clears inactive fields when switching between app and traffic targets', async () => {
    const user = await showPage();
    await newEditor(user);
    await user.selectOptions(screen.getByRole('combobox', { name: '分流链接' }), 'launch-route');
    await chooseURL(user);
    await saveNew(user);
    expect(mutations[0].body).toEqual(appDraft);
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

  it('switches all three statuses from the list while preserving configured rules', async () => {
    const rules = { allowed_ips: ['192.0.2.1'], blocked_ips: ['2001:db8::1'], blocked_languages: ['zh-cn'], block_empty_language: true, blocked_browsers: ['edge', 'firefox'], blocked_countries: ['US'], blocked_uas: ['curl'], allowed_platforms: ['google', 'bigo'], blocked_devices: [{ device: 'mobile' as const, systems: ['android'] }] };
    rows = [fixture(rules)];
    const user = await showPage();
    for (const status of ['off', 'blocked', 'rules']) {
      await user.selectOptions(screen.getByRole('combobox', { name: '斗篷状态：海外应用推广' }), status);
      await waitFor(() => expect(rows[0].status).toBe(status));
      expect((screen.getByRole('combobox', { name: '斗篷状态：海外应用推广' }) as HTMLSelectElement).value).toBe(status);
    }
    expect(mutations.map(item => ({ status: item.body?.status, enabled: item.body?.enabled }))).toEqual([{ status: 'off', enabled: false }, { status: 'blocked', enabled: false }, { status: 'rules', enabled: true }]);
    for (const { body } of mutations) {
      expect(Object.keys(body!).sort()).toEqual(editableKeys);
      expect(body).toMatchObject(rules);
    }
  });

  it('saves all three editor statuses, previews their routing, and preserves IP lists', async () => {
    const ips = { allowed_ips: ['192.0.2.1'], blocked_ips: ['2001:db8::1'] };
    rows = [fixture(ips)];
    const user = await showPage();
    for (const status of ['off', 'blocked', 'rules']) {
      await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
      await user.selectOptions(screen.getByRole('combobox', { name: '斗篷状态', exact: true }), status);
      const summary = within(screen.getByRole('complementary'));
      if (status === 'off') expect(summary.getAllByText('跳过全部斗篷规则，包括 IP 黑名单，访问目标链接。').length).toBeGreaterThan(0);
      if (status === 'blocked') {
        expect(summary.getAllByText('全部访问进入安全页，包括 IP 白名单。').length).toBeGreaterThan(0);
        expect(summary.queryByText('目标链接')).toBeNull();
        expect(summary.getByText('全部访问')).toBeTruthy();
      }
      if (status === 'rules') expect(summary.getByText('已设置 1 类规则')).toBeTruthy();
      await user.click(screen.getByRole('button', { name: '保存修改' }));
      await screen.findByText('斗篷已保存，入口可以使用了');
      expect(mutations.at(-1)?.body).toMatchObject({ ...ips, status, enabled: status === 'rules' });
    }
  });

  it('uses explicit status over the legacy enabled flag', async () => {
    rows = [fixture({ status: 'blocked', enabled: true })];
    const user = await showPage();
    expect((screen.getByRole('combobox', { name: '斗篷状态：海外应用推广' }) as HTMLSelectElement).value).toBe('blocked');
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    expect((screen.getByRole('combobox', { name: '斗篷状态', exact: true }) as HTMLSelectElement).value).toBe('blocked');
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[0].body).toMatchObject({ status: 'blocked', enabled: false });
  });

  it('maps old enabled responses to rules or off and persists an explicit status', async () => {
    const legacy = fixture({ enabled: false });
    Reflect.deleteProperty(legacy, 'status');
    rows = [legacy];
    const user = await showPage();
    expect((screen.getByRole('combobox', { name: '斗篷状态：海外应用推广' }) as HTMLSelectElement).value).toBe('off');
    await user.click(screen.getByRole('button', { name: '编辑', exact: true }));
    expect((screen.getByRole('combobox', { name: '斗篷状态', exact: true }) as HTMLSelectElement).value).toBe('off');
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await screen.findByText('斗篷已保存，入口可以使用了');
    expect(mutations[0].body).toMatchObject({ status: 'off', enabled: false });
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
    await user.selectOptions(screen.getByRole('combobox', { name: '斗篷状态', exact: true }), 'off');
    await user.click(screen.getByRole('button', { name: '保存并生成入口' }));
    expect((await screen.findByRole('alert')).textContent).toContain('目标页和安全页不能填写本站斗篷入口');
    expect((screen.getByRole('textbox', { name: '斗篷名称', exact: true }) as HTMLInputElement).value).toBe('  海外应用推广  ');
    expect((screen.getByRole('combobox', { name: '应用链接', exact: true }) as HTMLSelectElement).value).toBe(appDraft.target_id);
    expect((screen.getByRole('textbox', { name: /安全页 URL/ }) as HTMLInputElement).value).toBe(baseDraft.safe_url);
    expect((screen.getByRole('checkbox', { name: /^日语/ }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole('combobox', { name: '斗篷状态', exact: true }) as HTMLSelectElement).value).toBe('off');
    await user.selectOptions(screen.getByRole('combobox', { name: '应用链接', exact: true }), 'b'.repeat(32));
    await saveNew(user);
    await waitFor(() => expect(mutations).toHaveLength(2));
    expect(mutations[1].body).toEqual({ ...appDraft, status: 'off', enabled: false, target_id: 'b'.repeat(32), blocked_languages: ['ja'] });
  });
});
