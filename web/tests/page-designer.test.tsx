import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { PageDesigner, type PageDesign } from '../src/page-designer';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';
import { readPagePicture } from '../src/page-images';
import { pageButtonText, pageLanguageOptions } from '../src/page-language';
import { getLocale, setLocale, t } from '../src/i18n';
import { defaultReturnFlow } from '../src/return-flow';

vi.mock('../src/download-domain', () => ({ useDownloadDomain: () => ({ data: { download_base_url: 'http://localhost:8088' } }) }));
vi.mock('../src/page-images', () => ({ readPagePicture: vi.fn() }));
vi.mock('../src/landing-source-selector', () => ({ LandingSourceSelector: ({ value, onChange }: { value?: {page_id:string;revision_id:string}; onChange:(value:{page_id:string;revision_id:string}|undefined)=>void }) => <label>测试DIY版本<select required data-field="diy_landing" value={value?.revision_id || ''} onChange={event=>onChange(event.target.value?{page_id:'page-one',revision_id:event.target.value}:undefined)}><option value="">请选择</option><option value="revision-two">活动页 v2</option></select></label> }));
vi.mock('../src/diy-landing-preview',()=>({DIYLandingPreview:({reference}:{reference?:{page_id:string;revision_id:string}})=><div data-testid="diy-preview">{reference?.revision_id}</div>}));
const clients: QueryClient[] = [];
it('binds DIY globally and keeps external-browser and return-flow configuration',async()=>{
 const state=renderDesigner({title:'',external_browser:{enabled:true,url:'https://destination.example'},return_flow_config:defaultReturnFlow()}),user=userEvent.setup();
 await user.click(await screen.findByRole('button',{name:'DIY落地页'}));
 expect(screen.queryByLabelText('展示名称')).toBeNull();
 expect(screen.queryByLabelText('下载按钮文字')).toBeNull();
 await user.selectOptions(screen.getByLabelText('测试DIY版本'),'revision-two');
 expect(screen.getByTestId('diy-preview').textContent).toBe('revision-two');
 await user.click(screen.getByRole('button',{name:'保存并发布'}));
 await screen.findByRole('heading',{name:'应用列表'});
 expect(state.published).toMatchObject({layout:'diy',diy_landing:{page_id:'page-one',revision_id:'revision-two'},external_browser:{enabled:true,url:'https://destination.example'},return_flow_config:{enabled:false}});
});
it('shares the PWA multi-language picker, preserves separate APK content and saves its priority',async()=>{
 const state=renderDesigner(),user=userEvent.setup();await screen.findByLabelText('展示名称');
 expect(screen.getByRole('heading',{name:'多语言落地页'})).toBeTruthy();
 expect(screen.getByRole('checkbox',{name:'Français'})).toBeTruthy();
 await user.click(screen.getByRole('checkbox',{name:'English',exact:true}));
 const title=screen.getByLabelText('展示名称');await user.clear(title);await user.type(title,'English app');
 expect((screen.getByRole('textbox',{name:'下载按钮文字',exact:true}) as HTMLInputElement).value).toBe('Download app');
 await user.click(screen.getByRole('button',{name:'简体中文',exact:true}));
 expect((screen.getByLabelText('展示名称') as HTMLInputElement).value).toBe('测试应用');
 await user.click(screen.getByRole('button',{name:'English',exact:true}));
 expect((screen.getByLabelText('展示名称') as HTMLInputElement).value).toBe('English app');
 await user.click(screen.getByRole('button',{name:'提高语言优先级'}));
 await user.click(screen.getByRole('button',{name:'保存并发布'}));await screen.findByRole('heading',{name:'应用列表'});
 expect(state.published?.language_priority).toEqual(['en-US','zh-CN']);
 expect(state.published?.localizations?.map(v=>[v.language,v.title])).toEqual([['en-US','English app'],['zh-CN','测试应用']]);
 expect(state.published?.localizations?.every(v=>!v.localizations)).toBe(true);
});
afterEach(() => { cleanup(); clients.splice(0).forEach(client => client.clear()); vi.unstubAllGlobals(); vi.mocked(readPagePicture).mockReset(); setLocale('zh-CN', false); });
it('previews drafts separately and publishes only when saved', async () => {
 const design: PageDesign = { layout: 'store', title: '原始标题', developer: '', summary: '', description: '', icon: '', screenshots: [], tags: [], button_text: '下载应用', theme_color: '#246b55', background_color: '#f4f7f5', show_description: true, show_screenshots: true, show_file_info: true, show_downloads: true };
 let published: PageDesign | undefined;
 const requests: string[] = [];
 vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
  requests.push(String(input));
  let body: unknown = {};
  if (String(input).endsWith('/preview')) body = { html: '<html><body>预览</body></html>' };
  else if (options?.method === 'PUT') { published = JSON.parse(String(options.body)); body = { design: published }; }
  else body = { app: { id: 'app1', slug: 'abc123', name: '测试应用' }, design, defaults: design };
  return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
 }));
 const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
 render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/apps/app1/design']}><ConfigContext.Provider value={{ config: { development: true, public_url: 'http://localhost:8088' } as Config, reload: vi.fn() }}><Routes><Route path="/apps/:id/design" element={<PageDesigner />} /><Route path="/apps" element={<h1>应用列表</h1>} /></Routes></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
 const user = userEvent.setup();
 const title = await screen.findByLabelText('展示名称');
 await user.clear(title); await user.type(title, '编辑后的标题');
 await waitFor(() => expect(requests.some(p => p.endsWith('/preview'))).toBe(true));
 expect(published).toBeUndefined();
 const frame = screen.getByTitle('应用详情页实时预览'); expect(frame.getAttribute('sandbox')).toBe('');
 await user.click(screen.getByRole('button', { name: '桌面' }));
 expect(frame.parentElement?.className).toContain('desktop');
 expect(screen.getByRole('link', { name: /查看已发布页面/ }).getAttribute('href')).toBe('http://localhost:8088/a/abc123/info');
 await user.click(screen.getByRole('button', { name: '保存并发布' }));
 await screen.findByRole('heading', { name: '应用列表' });
 expect(published?.title).toBe('编辑后的标题');
 client.clear();
});

it('focuses a required title instead of silently refusing a bottom Save',async()=>{
 const state=renderDesigner(),user=userEvent.setup();const title=await screen.findByLabelText('展示名称');
 await user.clear(title);await user.click(screen.getByRole('button',{name:'保存并发布'}));
 expect(document.activeElement).toBe(title);expect(state.published).toBeUndefined();
 await user.type(title,'Corrected');await user.click(screen.getByRole('button',{name:'保存并发布'}));
 await screen.findByRole('heading',{name:'应用列表'});expect(state.published?.title).toBe('Corrected');
});
it('switches back to the language whose hidden title is invalid on Save',async()=>{
 const state=renderDesigner(),user=userEvent.setup();const title=await screen.findByLabelText('展示名称');
 await user.click(screen.getByRole('checkbox',{name:'English',exact:true}));
 await user.clear(title);
 await user.click(screen.getByRole('button',{name:'简体中文',exact:true}));
 await user.click(screen.getByRole('button',{name:'保存并发布'}));
 await waitFor(()=>expect(document.activeElement).toBe(screen.getByLabelText('展示名称')));
 expect((screen.getByLabelText('展示名称') as HTMLInputElement).value).toBe('');
 expect(state.published).toBeUndefined();
});

function renderDesigner(overrides: Partial<PageDesign> = {}) {
 const design: PageDesign = { layout: 'google_play', title: '测试应用', developer: '', summary: '', description: '', icon: '', screenshots: [], tags: [], button_text: '下载应用', theme_color: '#01875f', background_color: '#ffffff', show_description: true, show_screenshots: true, show_file_info: true, show_downloads: true, ...overrides };
 const state: { published?: PageDesign; previews: PageDesign[]; uploads: FormData[] } = { previews: [], uploads: [] };
 vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
  let body: unknown;
  if (String(input).endsWith('/page-images')) { state.uploads.push(options?.body as FormData); body = { url: `https://images.okpwa.cc/d1apk/page-images/test/${state.uploads.length}.jpg` }; }
  else if (String(input).endsWith('/preview')) { state.previews.push(JSON.parse(String(options?.body))); body = { html: '<html><body>预览</body></html>' }; }
  else if (options?.method === 'PUT') { state.published = JSON.parse(String(options.body)); body = { design: state.published }; }
  else body = { app: { id: 'app1', slug: 'abc123', name: '测试应用', downloads: 37 }, design, defaults: design };
  return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
 }));
 const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
 render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/apps/app1/design']}><ConfigContext.Provider value={{ config: { development: true, public_url: 'http://localhost:8088' } as Config, reload: vi.fn() }}><Routes><Route path="/apps/:id/design" element={<PageDesigner />} /><Route path="/apps" element={<h1>应用列表</h1>} /></Routes></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
 return state;
}

it('edits optional display ratings and download counts separately and only publishes on save', async () => {
 const state = renderDesigner(); const user = userEvent.setup();
 const rating = await screen.findByRole('spinbutton', { name: /展示评分/ });
 expect((rating as HTMLInputElement).value).toBe('');
 await user.type(rating, '4.8');
 await user.type(screen.getByRole('textbox', { name: /展示评价数/ }), '1.2万');
 await user.type(screen.getByRole('textbox', { name: /展示下载量/ }), '100K+');
 await waitFor(() => expect(state.previews.some(d => d.display_rating === '4.8' && d.display_download_count === '100K+')).toBe(true));
 expect(state.published).toBeUndefined();
 await user.click(screen.getByRole('button', { name: '保存并发布' }));
 await screen.findByRole('heading', { name: '应用列表' });
 expect(state.published).toMatchObject({ display_rating: '4.8', display_review_count: '1.2万', display_download_count: '100K+' });
 expect(state.published).not.toHaveProperty('downloads');
});

it('uploads processed pictures to the same-origin R2 endpoint and only saves returned URLs', async () => {
 const dataURL = 'data:image/jpeg;base64,' + btoa('test-compressed-image');
 vi.mocked(readPagePicture).mockResolvedValue(dataURL);
 const state = renderDesigner(); const user = userEvent.setup();
 await screen.findByLabelText('展示名称');
 const file = new File(['fixture'], 'icon.png', { type: 'image/png' });
 await user.upload(screen.getByLabelText('上传图标', { selector: 'input' }), file);
 await screen.findByRole('img', { name: '应用图标' });
 expect(readPagePicture).toHaveBeenCalledWith(file, true);
 await user.upload(screen.getByLabelText('上传截图', { selector: 'input' }), new File(['fixture'], 'screenshot.webp', { type: 'image/webp' }));
 await screen.findByRole('img', { name: '截图 1' });
 await waitFor(() => expect(state.previews.some(d => d.icon === 'https://images.okpwa.cc/d1apk/page-images/test/1.jpg' && d.screenshots[0] === 'https://images.okpwa.cc/d1apk/page-images/test/2.jpg')).toBe(true));
 expect(state.uploads).toHaveLength(2);
 const uploaded = state.uploads[0].get('file') as File;
 expect(uploaded.type).toBe('image/jpeg'); expect(uploaded.size).toBe('test-compressed-image'.length);
 expect(state.published).toBeUndefined();
 await user.click(screen.getByRole('button', { name: '保存并发布' }));
 await screen.findByRole('heading', { name: '应用列表' });
 expect(JSON.stringify(state.published)).not.toContain('data:');
 expect(state.published?.icon).toBe('https://images.okpwa.cc/d1apk/page-images/test/1.jpg');
});

it('shows a clear image error and preserves the existing icon when processing fails', async () => {
 const original = 'https://example.com/original.png';
 vi.mocked(readPagePicture).mockRejectedValue(new Error('图片无法读取，请确认文件未损坏，并重新选择 PNG、JPEG 或 WebP 图片'));
 const state = renderDesigner({ icon: original }); const user = userEvent.setup();
 await screen.findByLabelText('展示名称');
 await user.upload(screen.getByLabelText('上传图标', { selector: 'input' }), new File(['broken'], 'broken.png', { type: 'image/png' }));
 await screen.findByText(/图片无法读取，请确认文件未损坏/);
 expect(screen.getByRole('img', { name: '应用图标' }).getAttribute('src')).toBe(original);
 expect((screen.getByRole('button', { name: '保存并发布' }) as HTMLButtonElement).disabled).toBe(false);
 expect(state.published).toBeUndefined();
});

it('keeps the current icon and re-enables controls if R2 rejects the upload', async () => {
 vi.mocked(readPagePicture).mockResolvedValue('data:image/jpeg;base64,' + btoa('fixture'));
 const original = 'https://example.com/original.png';
 renderDesigner({ icon: original });
 const previous = vi.mocked(fetch).getMockImplementation()!;
 vi.stubGlobal('fetch', vi.fn((input: string, options?: RequestInit) => String(input).endsWith('/page-images')
  ? Promise.resolve(new Response(JSON.stringify({ error: 'R2 图片上传失败，请稍后重试' }), { status: 502 }))
  : previous(input, options)));
 const user = userEvent.setup();
 await screen.findByLabelText('展示名称');
 await user.upload(screen.getByLabelText('上传图标', { selector: 'input' }), new File(['fixture'], 'icon.png', { type: 'image/png' }));
 await screen.findByText('R2 图片上传失败，请稍后重试');
 expect(screen.getByRole('img', { name: '应用图标' }).getAttribute('src')).toBe(original);
 expect((screen.getByRole('button', { name: '保存并发布' }) as HTMLButtonElement).disabled).toBe(false);
});

it.each(pageLanguageOptions)('switches template copy to $label in one click, previews it and persists only on save', async option => {
 const original = { title: '客户自己的应用名称', description: '介绍不能被覆盖', icon: 'https://example.com/app.png', display_download_count: '100K+' };
 const state = renderDesigner({...original,layout:'store'}), user = userEvent.setup();
 await screen.findByLabelText('展示名称');
 expect(screen.getByRole('button', { name: '简体中文', pressed: true })).toBeTruthy();
 await user.click(screen.getByRole('button', { name: option.label, exact: true }));
 expect(screen.getByRole('button', { name: option.label, pressed: true })).toBeTruthy();
 expect((screen.getByRole('textbox', { name: '下载按钮文字', exact: true }) as HTMLInputElement).value).toBe(pageButtonText(option.value));
 await waitFor(() => expect(state.previews.some(draft => draft.language === option.value && draft.button_text === pageButtonText(option.value))).toBe(true));
 expect(state.previews.at(-1)).toMatchObject(original);
 expect(state.published).toBeUndefined();
 await user.click(screen.getByRole('button', { name: '保存并发布' }));
 await screen.findByRole('heading', { name: '应用列表' });
 expect(state.published).toMatchObject({ ...original, language: option.value, button_text: pageButtonText(option.value) });
});

it('preserves customized button wording and restores the selected language default on request', async () => {
 renderDesigner({ layout:'store', button_text: '领取专属安装包' }); const user = userEvent.setup();
 await screen.findByLabelText('展示名称');
 await user.click(screen.getByRole('button', { name: 'English', exact: true }));
 expect((screen.getByRole('textbox', { name: '下载按钮文字', exact: true }) as HTMLInputElement).value).toBe('领取专属安装包');
 await user.click(screen.getByRole('button', { name: '使用语言默认文案' }));
 expect((screen.getByRole('textbox', { name: '下载按钮文字', exact: true }) as HTMLInputElement).value).toBe('Download app');
 await user.click(screen.getByRole('button', { name: 'Deutsch', exact: true }));
 expect((screen.getByRole('textbox', { name: '下载按钮文字', exact: true }) as HTMLInputElement).value).toBe('App herunterladen');
});

it('keeps template language independent of console language and keeps it when restoring other defaults', async () => {
 renderDesigner({layout:'store'}); const user = userEvent.setup();
 await screen.findByLabelText('展示名称');
 await user.click(screen.getByRole('button', { name: '日本語', exact: true }));
 await user.click(screen.getByRole('button', { name: '恢复默认' }));
 expect(screen.getByRole('button', { name: '日本語', pressed: true })).toBeTruthy();
 expect((screen.getByRole('textbox', { name: '下载按钮文字', exact: true }) as HTMLInputElement).value).toBe('ダウンロード');
 setLocale('en', false);
 await screen.findByRole('heading', { name: t('详情页设计') });
 expect(getLocale()).toBe('en');
 expect(screen.getByRole('button', { name: '日本語', pressed: true })).toBeTruthy();
 expect((screen.getByRole('textbox', { name: t('下载按钮文字'), exact: true }) as HTMLInputElement).value).toBe('ダウンロード');
});

it('uses the reference APK templates and publishes the new content settings only on save',async()=>{
 const state=renderDesigner({layout:'custom',display_rating:'4.8'}),user=userEvent.setup();await screen.findByLabelText('展示名称');
 expect(screen.queryByRole('button',{name:'自定义图片模板'})).toBeNull();expect(screen.getByText('当前应用保留旧版模板。选择新模板后才会更换页面。')).toBeTruthy();
 await user.selectOptions(screen.getByLabelText('年龄分级'),'18');await user.selectOptions(screen.getByLabelText('按钮位置'),'top');await user.click(screen.getByLabelText('显示桌面二维码'));
 expect((screen.getByRole('checkbox',{name:/文字混淆/}) as HTMLInputElement).checked).toBe(false);
 await user.click(screen.getByRole('checkbox',{name:/文字混淆/}));
 expect(document.querySelector('iframe')?.getAttribute('src')).toContain('/api/pwa-landing/preview/custom?preview=true&apk=1');expect(state.published).toBeUndefined();
 await user.click(screen.getByRole('button',{name:'保存并发布'}));await screen.findByRole('heading',{name:'应用列表'});
 expect(state.published).toMatchObject({layout:'custom',landing:{age_rating:'18',button_position:'top',enable_qrcode:true,text_obfuscation:true}});
});

it('saves external browser behavior globally and focuses an invalid destination',async()=>{
 const state=renderDesigner(),user=userEvent.setup();await screen.findByLabelText('展示名称');
 await user.click(screen.getByLabelText('跳转外部浏览器'));
 await user.click(screen.getByRole('button',{name:'保存并发布'}));
 const target=screen.getByLabelText('外部浏览器目标网址');expect(document.activeElement).toBe(target);expect(state.published).toBeUndefined();
 await user.type(target,'https://shop.example/?campaign=1#/checkout');
 await user.click(screen.getByRole('checkbox',{name:'English',exact:true}));
 expect((screen.getByLabelText('外部浏览器目标网址') as HTMLInputElement).value).toBe('https://shop.example/?campaign=1#/checkout');
 await user.click(screen.getByRole('button',{name:'保存并发布'}));await screen.findByRole('heading',{name:'应用列表'});
 expect(state.published?.external_browser).toEqual({enabled:true,url:'https://shop.example/?campaign=1#/checkout'});
});
