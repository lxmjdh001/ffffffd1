import { useSyncExternalStore } from 'react';
import messages from './locales/messages.json' with { type: 'json' };

export const locales = ['en', 'pt', 'de', 'fr', 'ko', 'ja', 'zh-CN', 'zh-TW', 'ru'] as const;
export type Locale = typeof locales[number];
export const languageOptions: { value: Locale; label: string }[] = [
  { value: 'en', label: 'English' }, { value: 'pt', label: 'Português' },
  { value: 'de', label: 'Deutsch' }, { value: 'fr', label: 'Français' },
  { value: 'ko', label: '한국어' }, { value: 'ja', label: '日本語' },
  { value: 'zh-CN', label: '简体中文' }, { value: 'zh-TW', label: '繁體中文' },
  { value: 'ru', label: 'Русский' },
];
const preferenceKey = 'd1.locale';
const listeners = new Set<() => void>();
const catalogs = messages as Partial<Record<Locale, Record<string, string>>>;

export function installMessages(messages: Partial<Record<Locale, Record<string, string>>>) {
  Object.assign(catalogs, messages);
}

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && locales.includes(value as Locale);
}
function savedLocale(): Locale | undefined {
  try { const value = localStorage.getItem(preferenceKey); return isLocale(value) ? value : undefined; }
  catch { return undefined; }
}
let activeLocale: Locale = savedLocale() || 'zh-CN';
export function getLocale() { return activeLocale; }
export function useLocale() {
  return useSyncExternalStore(callback => { listeners.add(callback); return () => listeners.delete(callback); }, getLocale, () => 'zh-CN' as Locale);
}
export function setLocale(value: Locale, persist = true) {
  if (!isLocale(value)) return;
  if (persist) {
    try { localStorage.setItem(preferenceKey, value); } catch { /* Storage can be disabled. */ }
  }
  if (typeof document !== 'undefined') document.documentElement.lang = value;
  if (activeLocale !== value) { activeLocale = value; for (const notify of listeners) notify(); }
}

export function browserLocale(languages: readonly string[]): Locale {
  for (const language of languages) {
    const tag = language.toLowerCase();
    if (tag.startsWith('zh')) return /hant|tw|hk|mo/.test(tag) ? 'zh-TW' : 'zh-CN';
    const base = tag.split('-')[0];
    if (isLocale(base)) return base;
  }
  return 'en';
}
export function countryLocale(country: string): Locale | undefined {
  const groups: [Locale, string[]][] = [
    ['zh-CN', ['CN']], ['zh-TW', ['TW', 'HK', 'MO']], ['ja', ['JP']], ['ko', ['KR']],
    ['pt', ['PT', 'BR', 'AO', 'MZ', 'CV', 'GW', 'ST', 'TL']],
    ['de', ['DE', 'AT', 'LI', 'CH']], ['fr', ['FR', 'BE', 'LU', 'MC']],
    ['ru', ['RU', 'BY', 'KZ', 'KG']], ['en', ['US', 'GB', 'IE', 'AU', 'CA', 'NZ']],
  ];
  return groups.find(([, countries]) => countries.includes(country.toUpperCase()))?.[0];
}
export function initializeLocale(country?: string, languages?: readonly string[]) {
  const saved = savedLocale();
  if (saved) { setLocale(saved, false); return; }
  // During a rolling deployment, a legacy config without country information
  // retains the existing language rather than resetting a visitor's interface.
  if (country === undefined) return;
  const preferred = countryLocale(country) || browserLocale(languages || (typeof navigator !== 'undefined' ? navigator.languages : []));
  setLocale(preferred, false);
}

export function t(source: string, values: Record<string, unknown> = {}): string {
  const translated = activeLocale === 'zh-CN' ? source : catalogs[activeLocale]?.[source] || catalogs.en?.[source] || source;
  return translated.replace(/\{(\d+)\}/g, (token, key: string) => Object.prototype.hasOwnProperty.call(values, key) ? String(values[key] ?? '') : token);
}

// Translate only developer-owned static content, never customer names, uploaded
// descriptions, URLs, payment addresses or other API/user-supplied values.
export function translateContent<T>(value: T): T {
  if (typeof value === 'string') return (/[\u3400-\u9fff]/.test(value) ? t(value) : value) as T;
  if (Array.isArray(value)) return value.map(translateContent) as T;
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, translateContent(item)])) as T;
  return value;
}

export function localizedCountry(code: string, fallback: string) {
  if (getLocale() === 'zh-CN') return fallback;
  if (code === 'XX' || code === 'T1') return t(fallback);
  try { return new Intl.DisplayNames([getLocale()], { type: 'region' }).of(code) || fallback; } catch { return fallback; }
}
export function localizedLanguage(code: string, fallback: string) {
  if (getLocale() === 'zh-CN') return fallback;
  try { return new Intl.DisplayNames([getLocale()], { type: 'language' }).of(code) || fallback; } catch { return fallback; }
}

export function localizedPlanName(name: string) {
  if (['免费套餐', '免费版'].includes(name)) return t('免费套餐');
  if (['尊享套餐', '尊享版'].includes(name)) return t('尊享套餐');
  return name; // Administrator-customized names are content, not interface keys.
}
