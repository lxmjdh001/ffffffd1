import { ArrowDownToLine, Bell, Download, Eye, ShieldCheck, Wallet } from 'lucide-react';
import { t, useLocale } from './i18n';
import { Field, number } from './ui';
import type { Config, Settings, UsageBillingRates } from './types';
import './usage-billing.css';

type Rates = UsageBillingRates & { download_price_micros: number };
export function usagePrices(config: Rates) {
  return [
    { key: 'download_price_micros' as const, label: t('APK 访问 UV'), price: config.download_price_micros, icon: ArrowDownToLine },
    { key: 'pwa_install_price_micros' as const, label: t('PWA 安装'), price: config.pwa_install_price_micros ?? 30000, icon: Download },
    { key: 'cloak_uv_price_micros' as const, label: t('斗篷访问 UV'), price: config.cloak_uv_price_micros ?? 1000, icon: ShieldCheck },
    { key: 'return_flow_price_micros' as const, label: t('回流页展示'), price: config.return_flow_price_micros ?? 1000, icon: Eye },
    { key: 'pwa_push_price_micros' as const, label: t('PWA 消息推送'), price: config.pwa_push_price_micros ?? 1000, icon: Bell },
  ];
}
export const usagePrice = (micros: number) => (micros / 1e6).toFixed(6).replace(/\.?0+$/, '') || '0';

export function UsageRates({ config }: { config: Config }) {
  useLocale();
  return <section className="panel padded usage-pricing" aria-label={t('按量计费')}>
    <div className="usage-pricing-heading"><Wallet size={19} /><div><h2>{t('按量计费')}</h2><p>{t('全部功能免费开放，按实际用量扣费。')}</p></div></div>
    <div className="usage-rate-grid">{usagePrices(config).map(rate => <article key={rate.key}><rate.icon size={19} /><h3>{rate.label}</h3><strong>${usagePrice(rate.price)}</strong><small>{t(' / 次')}</small></article>)}</div>
    <p className="usage-gift-note">{t('APK 访问与 PWA 安装共用 {0} 次赠送额度。', { 0: number(config.registration_gift_limit ?? 1000) })}</p>
    <p className="small muted">{t('斗篷、回流页和消息推送从余额扣费。')}</p>
  </section>;
}

export function UsagePriceFields({ settings, change }: { settings: Settings; change: (key: keyof Settings, value: number) => void }) {
  useLocale();
  return <><div className="form-grid four">{usagePrices(settings).map(rate => <Field key={rate.key} label={rate.label + t('（美元 / 次）')}><input required aria-label={rate.label} type="number" min="0" max="1" step="0.000001" value={rate.price / 1e6} onChange={e => change(rate.key, Math.round(Number(e.target.value) * 1e6))} /></Field>)}</div>
    <div className="form-grid"><Field label={t('注册赠送额度（APK / PWA 共用）')}><input required type="number" min="0" max="100000000" step="1" value={settings.registration_gift_limit ?? 1000} onChange={e => change('registration_gift_limit', Number(e.target.value))}/></Field><Field label={t('统一应用名额')}><input required type="number" min="1" max="100000" step="1" value={settings.app_limit ?? 500} onChange={e => change('app_limit', Number(e.target.value))}/></Field><Field label={t('统一存储额度（GB）')}><input required type="number" min="0.01" max="102400" step="0.01" value={(settings.storage_limit_bytes ?? 30 * 2 ** 30) / 2 ** 30} onChange={e => change('storage_limit_bytes', Math.round(Number(e.target.value) * 2 ** 30))}/></Field></div></>;
}
