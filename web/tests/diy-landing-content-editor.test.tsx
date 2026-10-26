import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DIYLandingContentEditor } from '../src/diy-landing-content-editor';
import { setLocale } from '../src/i18n';
import type { DIYLandingContentSchema, DIYLandingReference } from '../src/diy-landing-types';
import { api } from '../src/api';

const schema: DIYLandingContentSchema = { fields: [
  { id: 'text-0', kind: 'title', label: '', value: '原始标题', max_length: 300 },
  { id: 'text-1', kind: 'text', label: '', value: '原始介绍', max_length: 16000 },
  { id: 'image-0', kind: 'image', label: '', value: 'https://assets.example/hero.png' },
  { id: 'primary-label', kind: 'button', label: '', value: '立即安装', max_length: 80 },
], max_image_bytes: 2 * 1024 * 1024, max_content_bytes: 8 * 1024 * 1024 };
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
const managedReference = '/return-flow-assets/0123456789abcdef0123456789abcdef.png';
const uploaded = { id: '0123456789abcdef0123456789abcdef', url: 'https://r2.example/diy/image.png', reference: managedReference, preview_url: managedReference, content_type: 'image/png', size: 123 };
beforeEach(() => { setLocale('zh-CN', false); vi.stubGlobal('fetch', vi.fn(async () => response(schema))); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function Editor({ initial = { page_id: 'page-one', revision_id: 'version-one' }, disabled = false, kind = 'apk' }: { initial?: DIYLandingReference; disabled?: boolean; kind?: 'apk' | 'pwa' } = {}) {
  const [reference, change] = useState<DIYLandingReference>(initial);
  return <><DIYLandingContentEditor kind={kind} reference={reference} change={change} disabled={disabled}/><output data-testid="saved-content">{JSON.stringify(reference)}</output></>;
}
const saved = () => JSON.parse(screen.getByTestId('saved-content').textContent || '{}') as DIYLandingReference;

it('edits content for the current application and keeps the primary action fixed', async () => {
  render(<Editor/>); const user = userEvent.setup();
  const title = await screen.findByRole('textbox', { name: '标题 1' });
  await user.clear(title); await user.type(title, '客户自己的标题');
  const copy = screen.getByRole('textbox', { name: '文案 1' }); await user.clear(copy); await user.type(copy, '新的介绍文案');
  const label = screen.getByRole('textbox', { name: '主按钮文案' }); await user.clear(label); await user.type(label, '下载体验');
  expect(saved()).toEqual({ page_id: 'page-one', revision_id: 'version-one', content: { 'text-0': '客户自己的标题', 'text-1': '新的介绍文案', 'primary-label': '下载体验' } });
  expect(screen.getByText('主按钮固定关联当前 APK，继续使用应用的下载和跳转设置。')).toBeTruthy();
  expect(screen.queryByLabelText(/按钮.*(?:链接|地址|动作)/)).toBeNull();
  expect(vi.mocked(fetch).mock.calls[0][0]).toBe('/api/diy-landings/page-one/revisions/version-one/content-fields?target=apk');
});

it('restores individual fields and all fields without submitting the surrounding form', async () => {
  const submit = vi.fn((event: {preventDefault: () => void}) => event.preventDefault());
  function Form() { const [reference, change] = useState<DIYLandingReference>({ page_id: 'page-one', revision_id: 'version-one', content: { 'text-0': '已修改标题', 'text-1': '已修改介绍' } }); return <form onSubmit={submit}><DIYLandingContentEditor kind="pwa" reference={reference} change={change}/><output data-testid="saved-content">{JSON.stringify(reference)}</output></form>; }
  render(<Form/>); const user = userEvent.setup(); await screen.findByRole('textbox', { name: '标题 1' });
  await user.click(screen.getByRole('button', { name: '恢复标题 1原始内容' }));
  expect(saved().content).toEqual({ 'text-1': '已修改介绍' });
  expect((screen.getByRole('textbox', { name: '标题 1' }) as HTMLInputElement).value).toBe('原始标题');
  await user.click(screen.getByRole('button', { name: '全部恢复' }));
  expect(saved()).toEqual({ page_id: 'page-one', revision_id: 'version-one' });
  expect(submit).not.toHaveBeenCalled();
  expect(screen.getByText('主按钮固定关联当前 PWA，继续使用应用的安装和跳转设置。')).toBeTruthy();
});

it('loads saved application overrides without modifying the template defaults', async () => {
  render(<Editor initial={{ page_id: 'page-one', revision_id: 'version-one', content: { 'text-0': '保存过的标题', 'primary-label': '立即下载' } }}/>);
  expect((await screen.findByRole('textbox', { name: '标题 1' }) as HTMLInputElement).value).toBe('保存过的标题');
  expect((screen.getByRole('textbox', { name: '主按钮文案' }) as HTMLInputElement).value).toBe('立即下载');
  expect(schema.fields[0].value).toBe('原始标题');
});

it('keeps an explicit empty image override and can restore the source image', async () => {
  render(<Editor/>); const user = userEvent.setup(); await screen.findByRole('textbox', { name: '图片 1' });
  await user.click(screen.getByRole('button', { name: '移除图片 1' }));
  expect(saved().content).toEqual({ 'image-0': '' });
  await user.click(screen.getByRole('button', { name: '恢复图片 1原始内容' }));
  expect(saved().content).toBeUndefined();
  expect((screen.getByRole('textbox', { name: '图片 1' }) as HTMLInputElement).value).toBe('https://assets.example/hero.png');
});

it('uploads images as multipart to R2 and stores only the managed reference', async () => {
  const busy = vi.fn(), change = vi.fn(), fileReader = vi.fn();
  let sent: RequestInit | undefined;
  vi.stubGlobal('FileReader',fileReader);
  vi.stubGlobal('fetch',vi.fn(async(input:string,options:RequestInit={})=>{
    if(String(input)==='/api/me')return response({csrf_token:'content-image-csrf'});
    if(String(input)==='/api/diy-landings/images'){sent=options;return response(uploaded);}
    return response(schema);
  }));
  await api('/me');
  render(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-one' }} change={change} onBusyChange={busy}/>);
  const input = await screen.findByLabelText('上传图片 1');
  await userEvent.setup().upload(input, new File(['test-image'], 'hero.png', { type: 'image/png' }));
  await waitFor(() => expect(change).toHaveBeenCalledWith(expect.objectContaining({ content: { 'image-0': managedReference } })));
  expect(sent?.method).toBe('POST');expect(sent?.body).toBeInstanceOf(FormData);
  const file=(sent?.body as FormData).get('file') as File;
  expect(file.name).toBe('hero.png');expect(file.size).toBe(10);expect(file.type).toBe('image/png');
  expect(new Headers(sent?.headers).get('X-CSRF-Token')).toBe('content-image-csrf');
  expect(new Headers(sent?.headers).has('Content-Type')).toBe(false);
  expect(sent?.credentials).toBe('same-origin');expect(fileReader).not.toHaveBeenCalled();
  expect(JSON.stringify(change.mock.calls)).not.toContain('data:image/');
  expect(busy).toHaveBeenCalledWith(true); await waitFor(() => expect(busy).toHaveBeenLastCalledWith(false));
});

it('displays managed R2 references without treating them as invalid HTTPS input',async()=>{
  const {container}=render(<Editor initial={{page_id:'page-one',revision_id:'version-one',content:{'image-0':managedReference}}}/>);
  const input=await screen.findByRole('textbox',{name:'图片 1'});
  expect((input as HTMLInputElement).value).toBe('');expect((input as HTMLInputElement).validity.valid).toBe(true);
  expect(input.getAttribute('placeholder')).toBe('已上传至 R2，可填写 HTTPS 地址替换');
  expect(container.querySelector('img')?.getAttribute('src')).toBe(managedReference);
  await userEvent.setup().click(screen.getByRole('button',{name:/编辑页面内容/}));
  expect(screen.queryByRole('textbox',{name:'图片 1'})).toBeNull();
  expect(saved().content).toEqual({'image-0':managedReference});
});

it('keeps legacy inline images visible and replaces them with an R2 reference on upload',async()=>{
  vi.stubGlobal('fetch',vi.fn(async(input:string)=>String(input)==='/api/diy-landings/images'?response(uploaded):response(schema)));
  const legacy='data:image/png;base64,bGVnYWN5';
  const {container}=render(<Editor initial={{page_id:'page-one',revision_id:'version-one',content:{'image-0':legacy}}}/>);
  const input=await screen.findByLabelText('上传图片 1');
  expect(container.querySelector('img')?.getAttribute('src')).toBe(legacy);
  expect(saved().content).toEqual({'image-0':legacy});
  await userEvent.setup().upload(input,new File(['new'],'new.png',{type:'image/png'}));
  await waitFor(()=>expect(saved().content).toEqual({'image-0':managedReference}));
  expect(container.querySelector('img')?.getAttribute('src')).toBe(managedReference);
});

it('preserves the original image when R2 upload fails and does not fall back to base64',async()=>{
  vi.stubGlobal('fetch',vi.fn(async(input:string)=>String(input)==='/api/diy-landings/images'?response({error:'R2 上传暂时不可用'},503):response(schema)));
  const busy=vi.fn(),change=vi.fn(),fileReader=vi.fn();vi.stubGlobal('FileReader',fileReader);
  render(<DIYLandingContentEditor kind="apk" reference={{page_id:'page-one',revision_id:'version-one',content:{'image-0':managedReference}}} change={change} onBusyChange={busy}/>);
  await userEvent.setup().upload(await screen.findByLabelText('上传图片 1'),new File(['image'],'hero.png',{type:'image/png'}));
  await screen.findByText('R2 上传暂时不可用');expect(change).not.toHaveBeenCalled();expect(fileReader).not.toHaveBeenCalled();
  expect(document.querySelector('.diy-content-image-thumb img')?.getAttribute('src')).toBe(managedReference);
  await waitFor(()=>expect(busy).toHaveBeenLastCalledWith(false));
});

it.each([
  {reference:'https://evil.example/image.png',preview_url:'https://evil.example/image.png'},
  {reference:'/a/paid-app/info',preview_url:'/a/paid-app/info'},
  {reference:'//evil.example/image.png',preview_url:'//evil.example/image.png'},
  {reference:managedReference+'?redirect=1',preview_url:managedReference+'?redirect=1'},
  {reference:managedReference+'\n',preview_url:managedReference+'\n'},
  {reference:managedReference.replace('.png','.svg'),preview_url:managedReference.replace('.png','.svg')},
  {reference:managedReference,preview_url:'https://evil.example/image.png'},
  {reference:managedReference,preview_url:'/a/paid-app/info'},
])('rejects an unsafe upload response without changing the image: $reference $preview_url',async result=>{
  vi.stubGlobal('fetch',vi.fn(async(input:string)=>String(input)==='/api/diy-landings/images'?response({...uploaded,...result}):response(schema)));
  const change=vi.fn();render(<DIYLandingContentEditor kind="apk" reference={{page_id:'page-one',revision_id:'version-one'}} change={change}/>);
  await userEvent.setup().upload(await screen.findByLabelText('上传图片 1'),new File(['image'],'hero.png',{type:'image/png'}));
  await screen.findByText('图片上传结果无效，请重新上传。');expect(change).not.toHaveBeenCalled();
  expect(document.querySelector('.diy-content-image-thumb img')).toBeNull();
});

it('locks controls during R2 upload and aborts stale upload responses after a source change',async()=>{
  let resolveUpload:((value:Response)=>void)|undefined;let signal:AbortSignal|null|undefined;
  vi.stubGlobal('fetch',vi.fn(async(input:string,options:RequestInit={})=>{
    if(String(input)==='/api/diy-landings/images'){signal=options.signal;return new Promise<Response>(resolve=>{resolveUpload=resolve;});}
    return response(schema);
  }));
  const change=vi.fn(),busy=vi.fn();
  const {rerender}=render(<DIYLandingContentEditor kind="apk" reference={{page_id:'page-one',revision_id:'version-one'}} change={change} onBusyChange={busy}/>);
  await userEvent.setup().upload(await screen.findByLabelText('上传图片 1'),new File(['image'],'hero.png',{type:'image/png'}));
  expect(screen.getByText('正在上传图片至 R2…')).toBeTruthy();
  expect(screen.getByRole('textbox',{name:'标题 1'}).matches(':disabled')).toBe(true);
  expect((screen.getByRole('button',{name:'全部恢复'}) as HTMLButtonElement).disabled).toBe(true);expect(busy).toHaveBeenCalledWith(true);
  rerender(<DIYLandingContentEditor kind="apk" reference={{page_id:'page-two',revision_id:'version-two'}} change={change} onBusyChange={busy}/>);
  await waitFor(()=>expect(screen.getByRole('textbox',{name:'标题 1'}).matches(':disabled')).toBe(false));
  expect(signal?.aborted).toBe(true);resolveUpload?.(response(uploaded));
  await waitFor(()=>expect(screen.queryByText('正在上传图片至 R2…')).toBeNull());
  expect(change).not.toHaveBeenCalled();expect(busy).toHaveBeenLastCalledWith(false);
});

it('aborts an in-flight R2 upload when the editor is removed',async()=>{
  let resolveUpload:((value:Response)=>void)|undefined;let signal:AbortSignal|null|undefined;
  vi.stubGlobal('fetch',vi.fn(async(input:string,options:RequestInit={})=>{
    if(String(input)==='/api/diy-landings/images'){signal=options.signal;return new Promise<Response>(resolve=>{resolveUpload=resolve;});}
    return response(schema);
  }));
  const change=vi.fn(),busy=vi.fn();
  const {unmount}=render(<DIYLandingContentEditor kind="apk" reference={{page_id:'page-one',revision_id:'version-one'}} change={change} onBusyChange={busy}/>);
  await userEvent.setup().upload(await screen.findByLabelText('上传图片 1'),new File(['image'],'hero.png',{type:'image/png'}));
  unmount();expect(signal?.aborted).toBe(true);resolveUpload?.(response(uploaded));
  expect(change).not.toHaveBeenCalled();expect(busy).toHaveBeenLastCalledWith(false);
});

it('ignores an old upload if an import replaces content while retaining the same page and version',async()=>{
  let resolveUpload:((value:Response)=>void)|undefined;
  vi.stubGlobal('fetch',vi.fn(async(input:string)=>String(input)==='/api/diy-landings/images'?new Promise<Response>(resolve=>{resolveUpload=resolve;}):response(schema)));
  const change=vi.fn(),busy=vi.fn();
  const initial={page_id:'page-one',revision_id:'version-one',content:{'text-0':'上传开始前'}};
  const {rerender}=render(<DIYLandingContentEditor kind="apk" reference={initial} change={change} onBusyChange={busy}/>);
  await userEvent.setup().upload(await screen.findByLabelText('上传图片 1'),new File(['image'],'hero.png',{type:'image/png'}));
  const importedImage='/return-flow-assets/ffffffffffffffffffffffffffffffff.webp';
  rerender(<DIYLandingContentEditor kind="apk" reference={{...initial,content:{'text-0':'导入后的内容','image-0':importedImage}}} change={change} onBusyChange={busy}/>);
  resolveUpload?.(response(uploaded));
  await waitFor(()=>expect(busy).toHaveBeenLastCalledWith(false));expect(change).not.toHaveBeenCalled();
  expect((screen.getByRole('textbox',{name:'标题 1'}) as HTMLInputElement).value).toBe('导入后的内容');
  expect(document.querySelector('.diy-content-image-thumb img')?.getAttribute('src')).toBe(importedImage);
});

it('accepts an upload after a same-source rerender with semantically unchanged content',async()=>{
  let resolveUpload:((value:Response)=>void)|undefined;
  vi.stubGlobal('fetch',vi.fn(async(input:string)=>String(input)==='/api/diy-landings/images'?new Promise<Response>(resolve=>{resolveUpload=resolve;}):response(schema)));
  const change=vi.fn(),busy=vi.fn();
  const initial={page_id:'page-one',revision_id:'version-one'};
  const {rerender}=render(<DIYLandingContentEditor kind="apk" reference={initial} change={change} onBusyChange={busy}/>);
  await userEvent.setup().upload(await screen.findByLabelText('上传图片 1'),new File(['image'],'hero.png',{type:'image/png'}));
  rerender(<DIYLandingContentEditor kind="apk" reference={{...initial,content:{}}} change={change} onBusyChange={busy}/>);
  resolveUpload?.(response(uploaded));
  await waitFor(()=>expect(change).toHaveBeenCalledWith({...initial,content:{'image-0':managedReference}}));
  await waitFor(()=>expect(busy).toHaveBeenLastCalledWith(false));
});

it('rejects oversized images before altering content', async () => {
  const change = vi.fn(); render(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-one' }} change={change}/>);
  const input = await screen.findByLabelText('上传图片 1');
  await userEvent.setup().upload(input, new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'large.png', { type: 'image/png' }));
  expect(screen.getByText('请选择不超过 2 MB 的 PNG、JPG、GIF 或 WebP 图片。')).toBeTruthy(); expect(change).not.toHaveBeenCalled();
});

it('rejects unsupported images even when a browser bypasses the accept filter', async () => {
  const change = vi.fn(); render(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-one' }} change={change}/>);
  const input = await screen.findByLabelText('上传图片 1');
  fireEvent.change(input, { target: { files: [new File(['<svg/>'], 'icon.svg', { type: 'image/svg+xml' })] } });
  expect(screen.getByText('请选择不超过 2 MB 的 PNG、JPG、GIF 或 WebP 图片。')).toBeTruthy(); expect(change).not.toHaveBeenCalled();
});

it('prevents edits while the application is being saved', async () => {
  const change = vi.fn(); render(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-one', content: { 'text-0': 'custom' } }} change={change} disabled/>);
  const title = await screen.findByRole('textbox', { name: '标题 1' });
  expect(title.matches(':disabled')).toBe(true); expect((screen.getByRole('button', { name: '全部恢复' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(title, { target: { value: 'should not change' } }); expect(change).not.toHaveBeenCalled();
});

it('enforces total content size before changing the reference', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ ...schema, max_content_bytes: 24 })));
  const change = vi.fn(); render(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-one' }} change={change}/>);
  fireEvent.change(await screen.findByRole('textbox', { name: '标题 1' }), { target: { value: 'x'.repeat(100) } });
  expect(screen.getByText('页面内容超过容量限制，请压缩图片后重试。')).toBeTruthy(); expect(change).not.toHaveBeenCalled();
});

it('aborts stale schema requests when the selected version changes', async () => {
  let finishOld: ((value: Response) => void) | undefined; const requests: RequestInit[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string, options: RequestInit = {}) => {
    requests.push(options);
    if (String(input).includes('version-one')) return new Promise<Response>(resolve => { finishOld = resolve; });
    return response({ fields: [{ id: 'new-title', kind: 'title', label: '', value: '新版本标题' }] });
  }));
  const { rerender } = render(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-one' }} change={vi.fn()}/>);
  rerender(<DIYLandingContentEditor kind="apk" reference={{ page_id: 'page-one', revision_id: 'version-two' }} change={vi.fn()}/>);
  expect((await screen.findByRole('textbox', { name: '标题 1' }) as HTMLInputElement).value).toBe('新版本标题');
  expect(requests[0].signal?.aborted).toBe(true); finishOld?.(response(schema));
  await waitFor(() => expect((screen.getByRole('textbox', { name: '标题 1' }) as HTMLInputElement).value).toBe('新版本标题'));
});

it('shows a schema error with an explicit retry', async () => {
  let calls = 0; vi.stubGlobal('fetch', vi.fn(async () => ++calls === 1 ? response({ error: '内容暂不可用' }, 503) : response(schema)));
  render(<Editor/>); await screen.findByText('内容暂不可用');
  await userEvent.setup().click(screen.getByRole('button', { name: '重新读取页面内容' }));
  expect((await screen.findByRole('textbox', { name: '标题 1' }) as HTMLInputElement).value).toBe('原始标题');
});

it('keeps explicitly empty button copy visible and invalid until it is restored',async()=>{
 render(<Editor/>);const user=userEvent.setup();
 const label=await screen.findByRole('textbox',{name:'主按钮文案'});
 expect((label as HTMLInputElement).validity.valid).toBe(true);
 await user.clear(label);expect((label as HTMLInputElement).validity.valueMissing).toBe(true);
 await user.click(screen.getByRole('button',{name:/编辑页面内容/}));
 expect(screen.getByRole('textbox',{name:'主按钮文案'})).toBe(label);
 await user.type(label,'   ');expect((label as HTMLInputElement).validity.patternMismatch).toBe(true);
 await user.click(screen.getByRole('button',{name:'恢复主按钮文案原始内容'}));
 expect((label as HTMLInputElement).validity.valid).toBe(true);expect(saved().content).toBeUndefined();
});

it('uses an explicit template field label without translating its content',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({fields:[{id:'title',kind:'title',label:'品牌名字 Brand',value:'My brand'}]})));
 render(<Editor/>);expect((await screen.findByRole('textbox',{name:'品牌名字 Brand'}) as HTMLInputElement).value).toBe('My brand');
});

it('never requests an arbitrary HTTPS thumbnail directly from the console',async()=>{
 const {container}=render(<Editor/>);await screen.findByRole('textbox',{name:'图片 1'});
 expect(container.querySelector('img[src="https://assets.example/hero.png"]')).toBeNull();
 expect(container.querySelector('img')).toBeNull();
});

it('shows default image thumbnails only through a signed preview resource URL',async()=>{
 const url='/api/landing-template-assets/content-fields?url=image&signature=signed';
 vi.stubGlobal('fetch',vi.fn(async()=>response({fields:[{id:'image-0',kind:'image',label:'',value:'https://assets.example/hero.png',preview_url:url}]})));
 const {container}=render(<Editor/>);await screen.findByRole('textbox',{name:'图片 1'});
 expect(container.querySelector('img')?.getAttribute('src')).toBe(url);
 fireEvent.change(screen.getByRole('textbox',{name:'图片 1'}),{target:{value:'https://assets.example/new.png'}});
 expect(container.querySelector('img')).toBeNull();
});

it('places the browser title before visible headings without changing their numbering',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({fields:[
  {id:'title-1',kind:'title',label:'',value:'可见大标题'},
  {id:'title-2',kind:'title',label:'',value:'可见副标题'},
  {id:'document-title',kind:'title',label:'ignored document label',value:'浏览器原始标题'},
 ]})));
 render(<Editor/>);
 const title=await screen.findByRole('textbox',{name:'网页标题'});
 expect((title as HTMLInputElement).value).toBe('浏览器原始标题');
 expect(screen.getAllByRole('textbox').map(node=>(node as HTMLInputElement).value)).toEqual(['浏览器原始标题','可见大标题','可见副标题']);
 expect((screen.getByRole('textbox',{name:'标题 1'}) as HTMLInputElement).value).toBe('可见大标题');
 expect((screen.getByRole('textbox',{name:'标题 2'}) as HTMLInputElement).value).toBe('可见副标题');
 const hint=screen.getByText('显示在浏览器标签上的标题，和页面内的大标题分别编辑。');
 expect(title.getAttribute('aria-describedby')).toBe(hint.id);
});

it('edits and restores the browser title independently of the visible page heading',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({fields:[
  {id:'title-1',kind:'title',label:'',value:'原始大标题'},
  {id:'document-title',kind:'title',label:'',value:'原始标签标题'},
 ]})));
 render(<Editor initial={{page_id:'page-one',revision_id:'version-one',content:{'title-1':'应用专属大标题'}}}/>);
 const user=userEvent.setup(),title=await screen.findByRole('textbox',{name:'网页标题'});
 await user.clear(title);await user.type(title,'应用专属标签标题');
 expect(saved().content).toEqual({'title-1':'应用专属大标题','document-title':'应用专属标签标题'});
 await user.clear(title);expect(saved().content).toEqual({'title-1':'应用专属大标题','document-title':''});
 expect((title as HTMLInputElement).validity.valid).toBe(true);
 await user.click(screen.getByRole('button',{name:'恢复网页标题原始内容'}));
 expect(saved().content).toEqual({'title-1':'应用专属大标题'});
 expect((title as HTMLInputElement).value).toBe('原始标签标题');
 expect((screen.getByRole('textbox',{name:'标题 1'}) as HTMLInputElement).value).toBe('应用专属大标题');
});

it('can add a browser title when the original page has none',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({fields:[
  {id:'document-title',kind:'title',label:'',value:''},
  {id:'title-1',kind:'title',label:'',value:'页面大标题'},
 ]})));
 render(<Editor/>);const user=userEvent.setup();
 const title=await screen.findByRole('textbox',{name:'网页标题'});
 expect((title as HTMLInputElement).value).toBe('');
 await user.type(title,'新增标签标题');expect(saved().content).toEqual({'document-title':'新增标签标题'});
 await user.clear(title);expect(saved().content).toEqual({'document-title':''});
 await user.click(screen.getByRole('button',{name:'恢复网页标题原始内容'}));
 expect(saved().content).toBeUndefined();
 expect((screen.getByRole('textbox',{name:'标题 1'}) as HTMLInputElement).value).toBe('页面大标题');
});
