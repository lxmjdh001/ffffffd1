import { Globe2 } from 'lucide-react';
import { languageOptions, setLocale, t, useLocale, type Locale } from './i18n';
import './language-selector.css';

export function LanguageSelector() {
  const locale = useLocale();
  return <label className="language-switcher"><Globe2 size={15} aria-hidden="true" /><span className="sr-only">{t('语言')}</span><select aria-label={t('语言')} value={locale} onChange={event => setLocale(event.target.value as Locale)}>{languageOptions.map(option => <option key={option.value} value={option.value} lang={option.value}>{option.label}</option>)}</select></label>;
}
