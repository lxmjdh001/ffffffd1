import type { AuthProvider, DataProvider } from 'react-admin';
import type { User, Plan } from './types';

let csrf = '';
export class APIError extends Error { constructor(message: string, public status: number) { super(message); } }
export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body && !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json');
  if (csrf) headers.set('X-CSRF-Token', csrf);
  const response = await fetch(`/api${path}`, { ...options, headers, credentials: 'same-origin' });
  const payload = await response.json().catch(() => ({ error: '服务器响应格式异常' }));
  // A cancelled identity request may finish after a new session is established.
  // Ignore its payload before it can restore an old CSRF token.
  options.signal?.throwIfAborted();
  if (!response.ok) throw new APIError(payload.error || '请求失败', response.status);
  if (payload.csrf_token) csrf = payload.csrf_token;
  return payload as T;
}
export const send = <T>(path: string, body: unknown, method = 'POST', key?: string) => api<T>(path, { method, body: JSON.stringify(body), headers: key ? { 'Idempotency-Key': key } : {} });
export const newKey = () => crypto.randomUUID().replaceAll('-', '');
export const getMe = (options: { signal?: AbortSignal } = {}) => api<{ user: User; plan: Plan; csrf_token: string }>('/me', { signal: options.signal });
export const authProvider: AuthProvider = {
  login: async ({ action, ...params }) => send<{ user: User; csrf_token: string }>(action === 'register' ? '/auth/register' : '/auth/login', params),
  logout: async () => { try { await api('/auth/logout', { method: 'POST' }); } catch (e) { if (!(e instanceof APIError) || e.status !== 401) throw e; } finally { csrf = ''; } },
  checkAuth: async params => { await getMe(params); },
  checkError: async error => { if (error.status === 401) throw error; },
  getIdentity: async params => { const { user } = await getMe(params); return { ...user, fullName: user.name }; },
  getPermissions: async params => {
    try { return (await getMe(params)).user.role; }
    catch (error) {
      // React Admin also queries permissions on public noLayout routes. An
      // anonymous visitor has no role; rejecting here would log them out and
      // redirect away from registration after the query retries finish.
      // Protected routes still enforce authentication through checkAuth.
      if (error instanceof APIError && error.status === 401) return null;
      throw error;
    }
  },
};
const unsupported = async (): Promise<never> => { throw new Error('该批量操作不受支持'); };
export const dataProvider: DataProvider = {
  getList: async (resource, params) => {
    const q = new URLSearchParams({ page: String(params.pagination?.page || 1), per_page: String(params.pagination?.perPage || 20), ...(params.filter || {}) });
    return api(`/${resource}?${q}`);
  },
  getOne: async (resource, params) => ({ data: await api(`/${resource}/${params.id}`) }),
  create: async (resource, params) => ({ data: await send(`/${resource}`, params.data) }),
  update: async (resource, params) => ({ data: await send(`/${resource}/${params.id}`, params.data, 'PATCH') }),
  delete: async (resource, params) => { await api(`/${resource}/${params.id}`, { method: 'DELETE' }); return { data: params.previousData! }; },
  getMany: unsupported, getManyReference: unsupported, updateMany: unsupported, deleteMany: unsupported,
};
export function upload(path: string, form: FormData, onProgress: (n: number) => void, signal?: AbortSignal): Promise<import('./types').App> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest(); xhr.open('POST', `/api${path}`); xhr.withCredentials = true; xhr.setRequestHeader('X-CSRF-Token', csrf);
    const abort = () => xhr.abort(); signal?.addEventListener('abort', abort, { once: true });
    xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress(Math.round(e.loaded / e.total * 100)); };
    xhr.onload = () => { signal?.removeEventListener('abort', abort); try { const data = JSON.parse(xhr.responseText); if (xhr.status >= 200 && xhr.status < 300) resolve(data); else reject(new APIError(data.error || '上传失败', xhr.status)); } catch { reject(new Error('上传未完成，请重试')); } };
    xhr.onerror = () => reject(new Error('网络错误，上传未完成')); xhr.onabort = () => reject(new Error('上传已取消')); xhr.timeout = 20 * 60 * 1000; xhr.ontimeout = () => reject(new Error('上传超时，请重试')); xhr.send(form);
  });
}
