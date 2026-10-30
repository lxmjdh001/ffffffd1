import { useState } from 'react';
import { t, useLocale } from './i18n';

// Web event vocabularies, checked against the providers' official references:
// Meta: github.com/meta/GoogleTagManager-WebTemplate-For-FacebookPixel/blob/main/template.tpl
// TikTok: ads.tiktok.com/help/article/standard-events-parameters?lang=en
// Download is custom for Meta; Install is custom for both web APIs.
const standardEvents = {
  facebook: ['CompleteRegistration', 'ViewContent', 'Lead', 'Contact', 'PageView', 'AddPaymentInfo', 'AddToCart', 'AddToWishlist', 'CustomizeProduct', 'Donate', 'FindLocation', 'InitiateCheckout', 'Purchase', 'Schedule', 'Search', 'StartTrial', 'SubmitApplication', 'Subscribe'],
  tiktok: ['Download', 'CompleteRegistration', 'ViewContent', 'Contact', 'AddPaymentInfo', 'AddToCart', 'AddToWishlist', 'ApplicationApproval', 'CustomizeProduct', 'FindLocation', 'InitiateCheckout', 'Purchase', 'Schedule', 'Search', 'StartTrial', 'SubmitApplication', 'SubmitForm', 'Subscribe'],
};

export function AdEventSelect({ channel, value, onChange }: { channel: 'facebook' | 'tiktok'; value: string; onChange: (value: string) => void }) {
  useLocale();
  const [custom, setCustom] = useState(false);
  const events = [...new Set(['Download', 'Install', ...standardEvents[channel], ...(value ? [value] : [])])];
  const label = (event: string) => {
    if (event === 'Download') return t('下载');
    if (event === 'Install') return t('安装');
    if (event === 'CompleteRegistration') return `${t('注册成功')} (CompleteRegistration)`;
    if (event === 'ViewContent') return `${t('应用打开')} (ViewContent)`;
    return event;
  };
  return <div className="ad-event-select"><select required value={custom ? '@custom' : value} onChange={e => { const next = e.target.value; setCustom(next === '@custom'); if (next !== '@custom') onChange(next); }}>{!value && <option value="">{t('回传事件')}</option>}{events.map(event => <option key={event} value={event}>{label(event)}</option>)}<option value="@custom">{t('自定义')}</option></select>{custom && <input required aria-label={t('自定义')} value={value} onChange={e => onChange(e.target.value)} maxLength={64} autoComplete="off" />}</div>;
}
