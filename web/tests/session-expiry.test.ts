import { afterEach, expect, it, vi } from 'vitest';
import { api, authProvider } from '../src/api';
import { setLocale } from '../src/i18n';

afterEach(() => { vi.unstubAllGlobals(); setLocale('zh-CN', false); });

it.each(['请先登录', '登录已过期，请重新登录', '登录过期，请重新登录'])('shows a clear login expiry message for %s', async message => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: message }), { status: 401 })));
  setLocale('zh-CN', false);
  await expect(authProvider.checkAuth({})).rejects.toMatchObject({ status: 401, message: '登录过期，请重新登录' });
  setLocale('en', false);
  await expect(authProvider.getIdentity!({})).rejects.toMatchObject({ status: 401, message: 'Your login has expired. Please sign in again.' });
});

it('uses the same clear message when the admin framework receives a bare 401', async () => {
  setLocale('zh-CN', false);
  await expect(authProvider.checkError({ status: 401 })).rejects.toMatchObject({ status: 401, message: '登录过期，请重新登录' });
});

it.each([
  ['/auth/login', 401, '账号或密码错误'],
  ['/me', 401, '账户不可用'],
  ['/me', 403, '安全令牌失效，请刷新页面'],
  ['/me', 500, '服务暂时不可用，请稍后重试'],
])('keeps %s errors (%s) distinct from an expired login', async (path, status, message) => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: message }), { status })));
  await expect(api(path)).rejects.toMatchObject({ status, message });
});
