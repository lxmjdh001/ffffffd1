import { validExternalBrowserURL } from './external-browser';
import type { FormEvent } from 'react';
import { t } from './i18n';
import { validReturnFlowURL } from './return-flow';

type Input = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;
export function focusField(field: HTMLElement | null | undefined, message?: string) {
 if (!field) return;
 field.scrollIntoView?.({ behavior: 'smooth', block: 'center' });
 field.focus({ preventScroll: true });
 if (message && 'setCustomValidity' in field) {
  const input = field as Input;
  input.setCustomValidity(message); input.setAttribute('aria-invalid', 'true'); input.reportValidity();
 }
}
export function resetFieldErrors(root: HTMLElement | null) {
 for (const field of root?.querySelectorAll<Input>('[aria-invalid="true"]') || []) { field.setCustomValidity?.(''); field.removeAttribute('aria-invalid'); }
}
export function clearFieldError(event: FormEvent<HTMLElement>) {
 const field = event.target;
 if (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement || field instanceof HTMLSelectElement) {
  field.setCustomValidity(''); field.removeAttribute('aria-invalid');
 }
}
export function firstInvalid(root: HTMLElement | null): boolean {
 for (const field of root?.querySelectorAll<Input>('input,textarea,select') || []) {
  if (field.disabled || field.type === 'file' || !field.willValidate) continue;
  if (field.dataset.validation === 'external' && !validExternalBrowserURL(field.value)) field.setCustomValidity(t('请输入有效的外部浏览器目标网址（HTTP/HTTPS）'));
  const trimmedRequired = field.required && !field.value.trim();
  if (field.dataset.validation === 'link' && field.value && !validReturnFlowURL(field.value)) field.setCustomValidity(t('PWA 落地页素材或链接无效'));
  const tooLong = 'maxLength' in field && field.maxLength >= 0 && [...field.value].length > field.maxLength;
  if (!field.validity.valid || trimmedRequired || tooLong) {
   if (trimmedRequired) field.setCustomValidity(t('请填写此项'));
   if (tooLong) field.setCustomValidity(t('请检查填写内容的长度'));
   field.setAttribute('aria-invalid', 'true'); focusField(field); field.reportValidity(); return true;
  }
 }
 return false;
}
// Server-side validation remains authoritative. Route its errors to the input
// when possible; otherwise bring the actionable error beside Save into view.
export function focusSaveError(root: HTMLElement | null, message: string) {
 const rules: [RegExp,string][] = [
  [/外部浏览器/, '[data-field="external-browser-url"]'],
  [/分享标识/, '[data-field="slug"]'], [/网页|HTTPS 网页|URL 必须|嵌入当前平台/, '[data-field="target_url"]'],
  [/应用名称/, '[data-field="name"]'], [/图标/, '[data-field="icon"]'],
  [/浏览器.*引导/, '[data-field="browser-guide"]'], [/APK.*地址|W2A/, '[data-field="apk-url"]'],
 ];
 requestAnimationFrame(() => {
 const selector = rules.find(([pattern]) => pattern.test(message))?.[1];
 const field = selector ? root?.querySelector<HTMLElement>(selector) : null;
 if (field) focusField(field,message);
 else { const alert = root?.querySelector<HTMLElement>('[role="alert"]'); if (alert) { alert.tabIndex = -1; focusField(alert); } }
 });
}
