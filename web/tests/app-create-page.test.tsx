import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppCreatePage, AppsPage, UploadDialog } from '../src/apps';
import { ConfigContext } from '../src/ui';
import { upload } from '../src/api';
import type { App, Config } from '../src/types';

const { list, notify } = vi.hoisted(() => ({ list: vi.fn(), notify: vi.fn() }));
vi.mock('react-admin', () => ({ useGetList: list, useNotify: () => notify }));
vi.mock('../src/api', async () => ({ ...await vi.importActual('../src/api'), upload: vi.fn() }));
let client: QueryClient;
beforeEach(() => {
 client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
 list.mockReturnValue({ data: [], total: 0, isPending: false });
 Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.setAttribute('open', ''); } });
 Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.removeAttribute('open'); } });
});
afterEach(() => { cleanup(); client.clear(); vi.mocked(upload).mockReset(); vi.restoreAllMocks(); });

function setup(path = '/apps', replace?: App) {
 const close = vi.fn();
 render(<QueryClientProvider client={client}><ConfigContext.Provider value={{ config: { max_upload_bytes: 10 * 1024 * 1024, development: true } as Config, reload: vi.fn() }}><MemoryRouter initialEntries={[path]}><Routes>
  <Route path="/apps" element={<AppsPage/>}/><Route path="/apps/new" element={<AppCreatePage/>}/><Route path="/apps/:id/design" element={<h1>详情页设计</h1>}/>
  <Route path="/replace" element={<UploadDialog app={replace} close={close} done={vi.fn()}/>}/>
 </Routes></MemoryRouter></ConfigContext.Provider></QueryClientProvider>);
 return { user: userEvent.setup(), close };
}

it.each([false, true])('opens a full creation page whether the account already has an APK: %s', async exists => {
 if (exists) list.mockReturnValue({ data: [{ id: 'old', slug: 'old-app', name: 'Existing APK', platform: 'Android', size: 100, downloads: 0, enabled: true }], total: 1, isPending: false });
 const { user } = setup();
 const links = screen.getAllByRole('link', { name: '上传应用' });
 expect(links.every(link => link.getAttribute('href') === '/apps/new')).toBe(true);
 await user.click(links[0]);
 expect(screen.getByRole('heading', { name: '上传新应用' })).toBeTruthy();
 expect(screen.queryByRole('dialog')).toBeNull();
 expect(screen.getByLabelText('应用名称')).toBeTruthy();
 await user.click(screen.getByRole('link', { name: '返回应用列表' }));
 expect(screen.getByRole('heading', { name: '应用管理' })).toBeTruthy();
});

it('sends old upload links to the same creation page', () => {
 setup('/apps?upload=1');
 expect(screen.getByRole('heading', { name: '上传新应用' })).toBeTruthy();
 expect(screen.queryByRole('dialog')).toBeNull();
});

it('publishes from the page and continues directly to detail-page design', async () => {
 const { user } = setup('/apps/new');
 await user.type(screen.getByLabelText('应用名称'), 'Second APK');
 await user.type(screen.getByLabelText(/分享标识/), 'second-app');
 await user.upload(document.querySelector<HTMLInputElement>('input[type=file]')!, new File(['APK'], 'new.apk', { type: 'application/vnd.android.package-archive' }));
 vi.mocked(upload).mockResolvedValue({ id: 'new-app' } as App);
 await user.click(screen.getByRole('button', { name: '上传并发布' }));
 await screen.findByRole('heading', { name: '详情页设计' });
 expect(upload).toHaveBeenCalledOnce();
 const [path, body] = vi.mocked(upload).mock.calls[0];
 expect(path).toBe('/apps'); expect(body.get('name')).toBe('Second APK'); expect(body.get('slug')).toBe('second-app');
});

it('keeps package replacement in its existing dialog and blocks closing mid-upload', async () => {
 const { user, close } = setup('/replace', { id: 'existing', name: 'Existing APK', version: '1.0.0' } as App);
 const dialog = within(screen.getByRole('dialog'));
 expect(dialog.getByRole('heading', { name: '更新应用安装包' })).toBeTruthy();
 await user.upload(screen.getByRole('dialog').querySelector<HTMLInputElement>('input[type=file]')!, new File(['APK'], 'update.apk', { type: 'application/vnd.android.package-archive' }));
 vi.mocked(upload).mockImplementation((_path, _body, _progress, signal) => new Promise((_resolve, reject) => { signal?.addEventListener('abort', () => reject(new Error('Upload cancelled'))); }));
 await user.click(dialog.getByRole('button', { name: '更新版本' }));
 await waitFor(() => expect(upload).toHaveBeenCalledOnce());
 await user.click(dialog.getByRole('button', { name: '关闭' })); expect(close).not.toHaveBeenCalled();
 await user.click(dialog.getByRole('button', { name: '取消上传' }));
 await waitFor(() => expect(dialog.getByRole('button', { name: '更新版本' })).toBeTruthy());
 expect(vi.mocked(upload).mock.calls[0][0]).toBe('/apps/existing/file');
});
