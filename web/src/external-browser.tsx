import { Field } from './ui';
import { t } from './i18n';

export type ExternalBrowserConfig = { enabled: boolean; url: string };

export function validExternalBrowserURL(value: string): boolean {
 const raw = value.trim();
 if (!/^https?:\/\//i.test(raw) || /[\\\x00-\x20\x7f-\x9f]/.test(raw) || new TextEncoder().encode(raw).length > 2048) return false;
 try {
  const url = new URL(raw);
  return !!url.hostname && !url.username && !url.password && !/^\/(?:a|p)\/[^/]+\/external\/open\/?$/.test(decodeURIComponent(url.pathname));
 } catch { return false; }
}

export function ExternalBrowserEditor({ value, change, disabled = false, pwa = false }: { value?: ExternalBrowserConfig; change: (next: ExternalBrowserConfig) => void; disabled?: boolean; pwa?: boolean }) {
 const config = value || { enabled: false, url: '' };
 return <section className={pwa ? 'pwa-creation-section' : 'panel padded'}>
  <h2>{t('外部浏览器打开')}</h2>
  <label className="switch-field"><input type="checkbox" checked={config.enabled} disabled={disabled} onChange={event => change({ ...config, enabled: event.target.checked })} /><span>{t('跳转外部浏览器')}</span></label>
  <p className="muted small">{t('开启后，点击详情页主按钮将在外部浏览器中打开目标网址，替代原下载或安装操作。')}</p>
  {config.enabled && <Field label={t('外部浏览器目标网址')} hint={t('请填写完整 HTTP/HTTPS 网址，支持参数和 # 路由。')}><input data-field="external-browser-url" data-validation="external" aria-label={t('外部浏览器目标网址')} type="url" required maxLength={2048} disabled={disabled} value={config.url} placeholder="https://example.com/" onChange={event => change({ ...config, url: event.target.value })} /></Field>}
  {config.enabled && <p className="muted small">{t('仅在点击时发起请求；应用可能限制外部打开。开启后暂停自动下载 APK。')}</p>}
 </section>;
}
