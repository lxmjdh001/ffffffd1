// i18n: static UI literals
import { Check, FileCode2, Play } from 'lucide-react';
import { t, useLocale } from './i18n';
import './landing-template-tabs.css';

export type LandingTemplateChoice = 'google_play' | 'diy';
export function LandingTemplateTabs({ value, onChange, disabled = false }: {
  value: string; onChange: (value: LandingTemplateChoice) => void; disabled?: boolean;
}) {
  useLocale();
  const legacy = value !== 'google_play' && value !== 'diy';
  return <div className="landing-template-choice">
    <div className="landing-template-tabs" role="group" aria-label={t('落地页模板')}>
      <button type="button" className={value === 'google_play' ? 'selected' : ''} disabled={disabled} aria-label={t('Google Play 商店模板')} aria-pressed={value === 'google_play'} onClick={() => onChange('google_play')}>
        <span className="landing-template-symbol"><Play size={25} fill="currentColor" strokeWidth={1.4}/></span>
        <span><strong>{t('Google Play 商店模板')}</strong><small>{t('使用应用资料生成商店详情页')}</small></span>
        {value === 'google_play' && <Check className="landing-template-check" size={17}/>}</button>
      <button type="button" className={value === 'diy' ? 'selected' : ''} disabled={disabled} aria-label={t('DIY落地页')} aria-pressed={value === 'diy'} onClick={() => onChange('diy')}>
        <span className="landing-template-symbol"><FileCode2 size={26} strokeWidth={1.5}/></span>
        <span><strong>{t('DIY落地页')}</strong><small>{t('选择已收藏模板，或制作自己的页面')}</small></span>
        {value === 'diy' && <Check className="landing-template-check" size={17}/>}</button>
    </div>
    {legacy && <p className="landing-template-legacy" role="status">{t('当前应用保留旧版模板。选择新模板后才会更换页面。')}</p>}
  </div>;
}
