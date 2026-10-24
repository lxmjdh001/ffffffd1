import { t } from './i18n';
import type { PWALandingConfig } from './pwa-landing';
import type { PageDesign } from './page-designer';
import { validReturnFlowURL } from './return-flow';

type Issue = { language: string; selector: string; message: string };
export function pwaLocaleIssue(config?: PWALandingConfig): Issue | undefined {
 for (const locale of config?.storeInfos || []) {
  const issue = (selector: string, message: string) => ({ language: locale.language, selector, message });
  if ([...locale.appName].length > 100) return issue('[data-field="store-name"]', t('请检查填写内容的长度'));
  if ([...locale.stayPromptText].length > 500) return issue('[data-field="stay-prompt"]', t('请检查填写内容的长度'));
  if (locale.tags.length > 30) return issue('[data-field="store-tags"]', t('PWA 落地页配置无效'));
  for (const [field, urls] of [['appIcon', [locale.appIcon]], ['detailImages', locale.detailImages], ['customImages', locale.customImages]] as const) {
   const index = urls.findIndex(url => url && !validReturnFlowURL(url));
   if (index >= 0) return issue(`[data-field="${field}-${index}"]`, t('PWA 落地页素材或链接无效'));
  }
 }
}

export function apkLocaleIssue(document: PageDesign): Issue | undefined {
 for (const locale of document.localizations?.length ? document.localizations : [document]) {
  const issue = (field: string, message: string) => ({ language: locale.language || 'zh-CN', selector: `[data-field="${field}"]`, message });
  for (const [field,max] of [['button_text',30],['title',100],['developer',80],['summary',200],['description',4000]] as const) {
   if ((['title','button_text'].includes(field) && !locale[field].trim()) || [...locale[field]].length > max) return issue(field, t('请检查标题、介绍和按钮文字的长度'));
  }
  if (locale.display_rating && !/^(?:[0-4](?:\.[0-9])?|5(?:\.0)?)$/.test(locale.display_rating)) return issue('display_rating', t('展示评分须为 0–5 分，最多一位小数，留空则不显示'));
  if (locale.tags.length > 6 || locale.tags.some(tag => [...tag].length > 20)) return issue('tags', t('以逗号分隔，最多 6 个，每个不超过 20 字。'));
  const validImage = (url: string) => /^data:image\/(png|jpeg);base64,/.test(url) || (/^https:\/\//.test(url) && validReturnFlowURL(url));
  if (locale.icon && !validImage(locale.icon)) return issue('icon', t('最多 5 张截图、6 个标签，图片支持 HTTPS 地址或 PNG/JPEG 上传'));
  if (locale.screenshots.some(url => !validImage(url))) return issue('screenshots', t('截图格式无效，请使用 HTTPS 图片或 PNG/JPEG'));
 }
}
