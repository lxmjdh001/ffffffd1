import { createI18n } from 'vue-i18n';
import zh from './locales/zh';
import en from './locales/en';
import es from './locales/es';
import fr from './locales/fr';
import de from './locales/de';
import ja from './locales/ja';
import ko from './locales/ko';
import pt from './locales/pt';
import ru from './locales/ru';
import it from './locales/it';
import ar from './locales/ar';
import hi from './locales/hi';
import tr from './locales/tr';
import vi from './locales/vi';
import th from './locales/th';
import pl from './locales/pl';
import nl from './locales/nl';
import id from './locales/id';
import sv from './locales/sv';
import uk from './locales/uk';
import ro from './locales/ro';
import cs from './locales/cs';

// 语言映射表
export const languageMap: Record<string, { name: string; nativeName: string }> = {
  en: { name: 'English', nativeName: 'English' },
  zh: { name: 'Chinese', nativeName: '中文' },
  es: { name: 'Spanish', nativeName: 'Español' },
  fr: { name: 'French', nativeName: 'Français' },
  de: { name: 'German', nativeName: 'Deutsch' },
  ja: { name: 'Japanese', nativeName: '日本語' },
  ko: { name: 'Korean', nativeName: '한국어' },
  pt: { name: 'Portuguese', nativeName: 'Português' },
  ru: { name: 'Russian', nativeName: 'Русский' },
  it: { name: 'Italian', nativeName: 'Italiano' },
  ar: { name: 'Arabic', nativeName: 'العربية' },
  hi: { name: 'Hindi', nativeName: 'हिन्दी' },
  tr: { name: 'Turkish', nativeName: 'Türkçe' },
  vi: { name: 'Vietnamese', nativeName: 'Tiếng Việt' },
  th: { name: 'Thai', nativeName: 'ไทย' },
  pl: { name: 'Polish', nativeName: 'Polski' },
  nl: { name: 'Dutch', nativeName: 'Nederlands' },
  id: { name: 'Indonesian', nativeName: 'Indonesia' },
  sv: { name: 'Swedish', nativeName: 'Svenska' },
  uk: { name: 'Ukrainian', nativeName: 'Українська' },
  ro: { name: 'Romanian', nativeName: 'Română' },
  cs: { name: 'Czech', nativeName: 'Čeština' }
};

// 检测浏览器语言
function detectBrowserLanguage(): string {
  const browserLang = navigator.language.toLowerCase();

  // 精确匹配
  if (languageMap[browserLang]) {
    return browserLang;
  }

  // 匹配语言代码前缀 (例如: en-US -> en)
  const langPrefix = browserLang.split('-')[0];
  if (languageMap[langPrefix]) {
    return langPrefix;
  }

  // 默认返回英语
  return 'en';
}

const i18n = createI18n({
  legacy: false,
  locale: detectBrowserLanguage(),
  fallbackLocale: 'en',
  messages: {
    zh,
    en,
    es,
    fr,
    de,
    ja,
    ko,
    pt,
    ru,
    it,
    ar,
    hi,
    tr,
    vi,
    th,
    pl,
    nl,
    id,
    sv,
    uk,
    ro,
    cs
  }
});

export default i18n;
