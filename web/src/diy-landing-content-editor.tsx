// i18n: static UI literals
import { useEffect, useId, useRef, useState, type ChangeEvent } from 'react';
import { ChevronDown, ImagePlus, Link2, LoaderCircle, Pencil, RotateCcw, ShieldCheck, Trash2 } from 'lucide-react';
import { api } from './api';
import { t, useLocale } from './i18n';
import { errorText, FormError } from './ui';
import type { DIYLandingContent, DIYLandingContentField, DIYLandingContentSchema, DIYLandingImageUpload, DIYLandingKind, DIYLandingReference } from './diy-landing-types';
import './diy-landing-content-editor.css';

const maxImageBytes = 2 * 1024 * 1024;
const maxContentBytes = 8 * 1024 * 1024;
const allowedImageTypes = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
const managedImage = (raw: string) => /^\/return-flow-assets\/[a-f0-9]{32}\.(?:png|jpe?g|gif|webp)$/.exec(raw)?.[0] === raw;
const groups = ['title', 'text', 'image', 'button'] as const;
const groupTitle = (kind: DIYLandingContentField['kind']) => kind === 'title' ? t('标题') : kind === 'text' ? t('页面文案') : kind === 'image' ? t('图片') : t('按钮');
const fieldTitle = (field: DIYLandingContentField, index: number) => field.id === 'document-title' ? t('网页标题') : field.label || (field.kind === 'button' ? t('主按钮文案') : field.kind === 'title' ? t('标题 {0}', { 0: index + 1 }) : field.kind === 'image' ? t('图片 {0}', { 0: index + 1 }) : t('文案 {0}', { 0: index + 1 }));
const own = (content: DIYLandingContent | undefined, id: string) => !!content && Object.prototype.hasOwnProperty.call(content, id);
const sameContent = (a: DIYLandingContent | undefined, b: DIYLandingContent | undefined) => {
  const keys = Object.keys(a || {});
  return keys.length === Object.keys(b || {}).length && keys.every(key => own(b, key) && a![key] === b![key]);
};
const imageSource = (raw: string, field: DIYLandingContentField) => {
  if (/^data:image\/(?:png|jpeg|gif|webp);base64,/i.test(raw)) return raw;
  if (managedImage(raw)) return raw;
  // HTTPS thumbnails use the same signed static proxy as the isolated preview.
  // Never make an arbitrary image URL a browser request from the console.
  if (raw === field.value && /^\/api\/landing-template-assets\/[A-Za-z0-9_-]+\?/.test(field.preview_url || '')) return field.preview_url!;
  return '';
};

function ImageThumbnail({ source }: { source: string }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [source]);
  return <div className="diy-content-image-thumb">{source && !failed ? <img src={source} alt="" referrerPolicy="no-referrer" onError={() => setFailed(true)}/> : <ImagePlus size={23} strokeWidth={1.5}/>}</div>;
}

type Props = {
  kind: DIYLandingKind; reference: DIYLandingReference; disabled?: boolean;
  change: (reference: DIYLandingReference) => void; onBusyChange?: (busy: boolean) => void;
};

export function DIYLandingContentEditor({ kind, reference, disabled = false, change, onBusyChange }: Props) {
  useLocale();
  const [schema, setSchema] = useState<DIYLandingContentSchema>(), [loading, setLoading] = useState(true), [error, setError] = useState('');
  const [expanded, setExpanded] = useState(true), [refresh, setRefresh] = useState(0), [uploading, setUploading] = useState(false);
  const fieldPrefix = useId(), bodyID = useId();
  const uploadRequest = useRef<AbortController | null>(null), mounted = useRef(true), referenceRef = useRef(reference), changeRef = useRef(change), busyRef = useRef(onBusyChange);
  referenceRef.current = reference; changeRef.current = change; busyRef.current = onBusyChange;
  const sourceKey = `${kind}:${reference.page_id}:${reference.revision_id}`;
  const sourceRef = useRef(sourceKey); sourceRef.current = sourceKey;

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; uploadRequest.current?.abort(); busyRef.current?.(false); };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    uploadRequest.current?.abort(); uploadRequest.current = null; setUploading(false); busyRef.current?.(false);
    setLoading(true); setSchema(undefined); setError('');
    api<DIYLandingContentSchema>(`/diy-landings/${encodeURIComponent(reference.page_id)}/revisions/${encodeURIComponent(reference.revision_id)}/content-fields?target=${kind}`, { signal: controller.signal })
      .then(result => { if (!controller.signal.aborted) setSchema({ ...result, fields: Array.isArray(result.fields) ? result.fields : [] }); })
      .catch(failure => { if (!controller.signal.aborted) setError(errorText(failure)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [kind, reference.page_id, reference.revision_id, refresh]);

  const update = (field: DIYLandingContentField, nextValue?: string) => {
    if (disabled || loading || !schema) return;
    const current = referenceRef.current;
    const content = { ...current.content };
    if (nextValue === undefined || field.id !== 'document-title' && nextValue === field.value) delete content[field.id];
    else content[field.id] = nextValue;
    const maximum = Math.min(schema.max_content_bytes || maxContentBytes, maxContentBytes);
    if (new TextEncoder().encode(JSON.stringify(content)).byteLength > maximum) { setError(t('页面内容超过容量限制，请压缩图片后重试。')); return; }
    const next = { ...current };
    if (Object.keys(content).length) next.content = content;
    else delete next.content;
    setError(''); changeRef.current(next);
  };
  const reset = () => {
    if (disabled || uploading || loading) return;
    const next = { ...referenceRef.current }; delete next.content; setError(''); changeRef.current(next);
  };
  const selectImage = async (event: ChangeEvent<HTMLInputElement>, field: DIYLandingContentField) => {
    const file = event.target.files?.[0]; event.target.value = '';
    if (!file || disabled || uploading || loading || !schema) return;
    const maximum = Math.min(schema.max_image_bytes || maxImageBytes, maxImageBytes);
    if (!allowedImageTypes.has(file.type) || file.size > maximum) { setError(t('请选择不超过 2 MB 的 PNG、JPG、GIF 或 WebP 图片。')); return; }
    const expectedSource = sourceRef.current;
    const expectedContent = { ...referenceRef.current.content };
    const controller = new AbortController(); uploadRequest.current = controller;
    const form = new FormData(); form.set('file', file);
    setError(''); setUploading(true); busyRef.current?.(true);
    try {
      const result = await api<DIYLandingImageUpload>('/diy-landings/images', { method: 'POST', body: form, signal: controller.signal });
      if (!mounted.current || controller.signal.aborted || sourceRef.current !== expectedSource || !sameContent(expectedContent, referenceRef.current.content)) return;
      if (!result || typeof result.reference !== 'string' || !managedImage(result.reference) || result.preview_url !== result.reference) {
        setError(t('图片上传结果无效，请重新上传。')); return;
      }
      update(field, result.reference);
    } catch (failure) {
      if (mounted.current && !controller.signal.aborted && sourceRef.current === expectedSource && sameContent(expectedContent, referenceRef.current.content)) setError(errorText(failure));
    } finally {
      if (uploadRequest.current === controller) {
        uploadRequest.current = null;
        if (mounted.current) { setUploading(false); busyRef.current?.(false); }
      }
    }
  };
  const changed = Object.keys(reference.content || {}).length;
  const hasInvalidContent = schema?.fields.some(field => {
    if (!own(reference.content, field.id)) return false;
    const value = reference.content![field.id];
    if (field.kind === 'button') return !value.trim();
    if (field.kind !== 'image' || !value || value.startsWith('data:') || managedImage(value)) return false;
    try { const url = new URL(value); return url.protocol !== 'https:' || !url.hostname || !!url.username || !!url.password; } catch { return true; }
  });
  const shown = expanded || !!hasInvalidContent;
  const locked = disabled || loading || uploading;

  return <section className="diy-content-editor" aria-label={t('落地页内容编辑')}>
    <div className="diy-content-heading"><button type="button" className="diy-content-toggle" aria-expanded={shown} aria-controls={bodyID} onClick={() => setExpanded(current => !current)}><Pencil size={16}/><span>{t('编辑页面内容')}<small>{changed ? t('已修改 {0} 项', { 0: changed }) : t('使用页面原始内容')}</small></span><ChevronDown size={17}/></button><button type="button" className="text-btn" disabled={locked || !changed} onClick={reset}><RotateCcw size={13}/>{t('全部恢复')}</button></div>
    <div id={bodyID} hidden={!shown}>
      <p className="diy-content-description">{t('修改仅用于当前应用，原模版和其他应用保持原样。保存应用后生效。')}</p>
      <div className="diy-content-action-lock"><ShieldCheck size={16}/><span>{kind === 'apk' ? t('主按钮固定关联当前 APK，继续使用应用的下载和跳转设置。') : t('主按钮固定关联当前 PWA，继续使用应用的安装和跳转设置。')}</span></div>
      {loading ? <p className="diy-content-loading" role="status"><LoaderCircle size={15} className="spin"/>{t('正在读取页面内容…')}</p> : schema && !schema.fields.length ? <p className="diy-content-empty">{t('没有可直接编辑的内容，可在DIY工作台修改页面代码。')}</p> : <div className="diy-content-groups">
        {groups.map(kind => {
          const fields = schema?.fields.filter(field => field.kind === kind) || [];
          if (kind === 'title') fields.sort((a, b) => a.id === 'document-title' ? -1 : b.id === 'document-title' ? 1 : 0);
          if (!fields.length) return null;
          return <fieldset key={kind} disabled={locked} className="diy-content-group"><legend>{groupTitle(kind)}<span>{fields.length}</span></legend>{fields.map((field, index) => {
            const number = kind === 'title' ? fields.slice(0, index).filter(entry => entry.id !== 'document-title').length : index;
            const label = fieldTitle(field, number), inputID = `${fieldPrefix}-${field.id}`;
            const value = own(reference.content, field.id) ? reference.content![field.id] : field.value;
            const altered = own(reference.content, field.id);
            return <div key={field.id} className={`diy-content-field ${field.kind === 'image' ? 'image' : ''}`}>
              <div className="diy-content-field-head"><label htmlFor={inputID}>{label}</label><button type="button" className="text-btn" disabled={locked || !altered} onClick={() => update(field)} aria-label={t('恢复{0}原始内容', { 0: label })}><RotateCcw size={12}/>{t('恢复原始')}</button></div>
              {field.id === 'document-title' && <p id={`${inputID}-hint`} className="diy-content-description">{t('显示在浏览器标签上的标题，和页面内的大标题分别编辑。')}</p>}
              {field.kind === 'image' ? <div className="diy-content-image-editor"><ImageThumbnail source={imageSource(value, field)}/><div className="diy-content-image-controls"><div className="diy-content-image-url"><Link2 size={14}/><input id={inputID} aria-label={label} data-field={`diy-content-${field.id}`} type="url" pattern="https://.*" maxLength={4096} value={value.startsWith('data:') || managedImage(value) ? '' : value} placeholder={managedImage(value) ? t('已上传至 R2，可填写 HTTPS 地址替换') : value.startsWith('data:') ? t('已使用上传图片，可填写 HTTPS 地址替换') : 'https://example.com/image.jpg'} onChange={event => update(field, event.target.value)}/></div><div className="diy-content-image-actions"><label className={`btn secondary ${locked ? 'disabled' : ''}`}><ImagePlus size={13}/>{t('替换图片')}<input aria-label={t('上传{0}', { 0: label })} type="file" hidden accept="image/png,image/jpeg,image/gif,image/webp" disabled={locked} onChange={event => { void selectImage(event, field); }}/></label><button type="button" className="text-btn" disabled={locked || !value} aria-label={t('移除{0}', { 0: label })} onClick={() => update(field, '')}><Trash2 size={13}/>{t('移除图片')}</button></div><small>{t('PNG、JPG、GIF、WebP，最大 2 MB。')}</small></div></div> : field.kind === 'text' ? <textarea id={inputID} aria-label={label} data-field={`diy-content-${field.id}`} rows={Math.min(5, Math.max(2, value.split('\n').length))} maxLength={field.max_length || 16000} value={value} onChange={event => update(field, event.target.value)}/> : <input id={inputID} aria-label={label} aria-describedby={field.id === 'document-title' ? `${inputID}-hint` : undefined} data-field={`diy-content-${field.id}`} required={field.kind === 'button' && altered} pattern={field.kind === 'button' ? '[\\s\\S]*\\S[\\s\\S]*' : undefined} maxLength={field.max_length || (field.kind === 'button' ? 80 : 300)} value={value} onChange={event => update(field, event.target.value)}/ >}
            </div>;
          })}</fieldset>;
        })}
      </div>}
      <FormError>{error}</FormError>{!loading && !schema && <button type="button" className="text-btn" disabled={disabled} onClick={() => setRefresh(value => value + 1)}>{t('重新读取页面内容')}</button>}
      {uploading && <p className="diy-content-loading" role="status"><LoaderCircle size={15} className="spin"/>{t('正在上传图片至 R2…')}</p>}
    </div>
  </section>;
}
