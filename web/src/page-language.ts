import messages from '../../internal/platform/templates/page-locales.json';

export type PageLanguage = keyof typeof messages;
export const pageLanguageOptions: { value: PageLanguage; label: string }[] = [
  { value: 'en', label: 'English' }, { value: 'zh-CN', label: '简体中文' },
  { value: 'zh-TW', label: '繁體中文' }, { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' }, { value: 'de', label: 'Deutsch' },
];

export function pageLanguage(value?: string): PageLanguage {
  return Object.prototype.hasOwnProperty.call(messages, value || '') ? value as PageLanguage : 'zh-CN';
}
export function pageButtonText(language?: string) { return messages[pageLanguage(language)].download_button; }

// Only system defaults follow a language change automatically. Preserve custom
// copy and every other publisher field, including names, images and statistics.
export function withPageLanguage<T extends { language?: string; button_text: string }>(draft: T, language: PageLanguage): T {
  const automatic = !draft.button_text.trim() || Object.values(messages).some(copy => copy.download_button === draft.button_text.trim());
  return { ...draft, language, button_text: automatic ? pageButtonText(language) : draft.button_text };
}
