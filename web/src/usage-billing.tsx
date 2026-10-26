import { useId, useState } from 'react';
import { ArrowDownToLine, Bell, ChevronDown, Download, Eye, Globe2, Link2, ShieldCheck, Wallet } from 'lucide-react';
import { t, useLocale } from './i18n';
import { Field } from './ui';
import type { Config, Settings, UsageBillingRates } from './types';
import './usage-billing.css';

type Rates = UsageBillingRates & { download_price_micros: number };
function paidUsagePrices(config: Rates) {
  return [
    { key: 'download_price_micros' as const, label: t('APK 下载'), price: config.download_price_micros, paid: true, icon: ArrowDownToLine },
    { key: 'pwa_install_price_micros' as const, label: t('PWA 安装'), price: config.pwa_install_price_micros ?? 50000, paid: true, icon: Download },
  ];
}
export function usagePrices(config: Rates) {
  return [
    ...paidUsagePrices(config),
    { key: 'cloak_uv_price_micros' as const, label: t('智能斗篷'), price: 0, paid: false, icon: ShieldCheck },
    { key: 'return_flow_price_micros' as const, label: t('回流页功能'), price: 0, paid: false, icon: Eye },
    { key: 'pwa_push_price_micros' as const, label: t('推送用量'), price: 0, paid: false, icon: Bell },
    { key: 'traffic_routing' as const, label: t('应用分流'), price: 0, paid: false, icon: Link2 },
    { key: 'domain_safety' as const, label: t('域名安全检测'), price: 0, paid: false, icon: Globe2 },
  ];
}
export const usagePrice = (micros: number) => (micros / 1e6).toFixed(6).replace(/\.?0+$/, '') || '0';

export function UsageRates({ config, compact = false, collapsible = false }: { config: Config; compact?: boolean; collapsible?: boolean }) {
  useLocale();
  const [expanded, setExpanded] = useState(false);
  const contentID = useId();
  return <section className={`panel padded usage-pricing${compact ? ' usage-pricing--compact' : ''}`} aria-label={t('按量计费')}>
    {collapsible ? <h2 className="usage-pricing-toggle-heading"><button type="button" className="usage-pricing-toggle" aria-expanded={expanded} aria-controls={contentID} onClick={() => setExpanded(value => !value)}><Wallet size={18} aria-hidden="true"/><span>{t('计费规则')}</span><span className="usage-pricing-toggle-action">{expanded ? t('收起') : t('展开')}<ChevronDown size={16} className="usage-pricing-toggle-chevron" aria-hidden="true"/></span></button></h2> : <div className="usage-pricing-heading"><Wallet size={19} /><div><h2>{t('按量计费')}</h2><p>{t('仅 APK 下载与 PWA 安装按量计费。')}</p></div></div>}
    <div id={contentID} hidden={collapsible && !expanded}>
      {(!collapsible || expanded) && <>{collapsible && <p className="usage-pricing-description">{t('仅 APK 下载与 PWA 安装按量计费。')}</p>}<div className="usage-rate-grid">{usagePrices(config).map(rate => <article className={rate.paid ? '' : 'usage-rate-free'} key={rate.key}><rate.icon size={19} /><h3>{rate.label}</h3>{rate.paid ? <div className="usage-rate-price"><strong>{'$' + usagePrice(rate.price)}</strong><small>{t(' / 次')}</small></div> : <strong>{t('免费')}</strong>}</article>)}</div></>}
    </div>
  </section>;
}

export function UsagePriceFields({ settings, change }: { settings: Settings; change: (key: keyof Settings, value: number) => void }) {
  useLocale();
  return <><div className="form-grid">{paidUsagePrices(settings).map(rate => <Field key={rate.key} label={rate.label + t('（美元 / 次）')}><input required aria-label={rate.label} type="number" min="0" max="1" step="0.000001" value={rate.price / 1e6} onChange={e => change(rate.key, Math.round(Number(e.target.value) * 1e6))} /></Field>)}</div>
    <div className="form-grid"><Field label={t('注册赠送额度（美元）')}><input required type="number" min="0" max="1000000" step="0.01" value={(settings.registration_gift_micros ?? 100e6)/1e6} onChange={e => change('registration_gift_micros', Math.round(Number(e.target.value)*1e6))}/></Field><Field label={t('统一应用名额')}><input required type="number" min="1" max="100000" step="1" value={settings.app_limit ?? 500} onChange={e => change('app_limit', Number(e.target.value))}/></Field><Field label={t('统一存储额度（GB）')}><input required type="number" min="0.01" max="102400" step="0.01" value={(settings.storage_limit_bytes ?? 30 * 2 ** 30) / 2 ** 30} onChange={e => change('storage_limit_bytes', Math.round(Number(e.target.value) * 2 ** 30))}/></Field></div></>;
}
