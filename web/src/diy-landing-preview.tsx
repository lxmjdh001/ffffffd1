// i18n: static UI literals
import { useEffect, useRef, useState } from 'react';
import { Expand, Monitor, RefreshCw, Smartphone } from 'lucide-react';
import { api } from './api';
import { t, useLocale } from './i18n';
import { errorText, FormError, Modal } from './ui';
import { defaultDIYLandingOptions, type DIYHTMLIssue, type DIYLandingKind, type DIYLandingOptions, type DIYLandingReference } from './diy-landing-types';
import './diy-landings.css';

type PreviewProps = {
  html?: string; reference?: DIYLandingReference; mode: DIYLandingKind; config?: DIYLandingOptions;
  onIssues?: (issues: DIYHTMLIssue[]) => void; refreshToken?: number; compact?: boolean;
};
type PreviewResponse = { html?: string; issues?: DIYHTMLIssue[]; preview_url?: string; src_url?: string; channel?: string };
const noIssues: DIYHTMLIssue[] = [];

export function DIYLandingPreview({ html, reference, mode, config, onIssues, refreshToken = 0, compact = false }: PreviewProps) {
  useLocale();
  const [result, setResult] = useState<PreviewResponse>();
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [width, setWidth] = useState(390), [full, setFull] = useState<'phone' | 'desktop'>(), [refresh, setRefresh] = useState(0);
  const normalStage = useRef<HTMLDivElement>(null);
  const [stageWidth, setStageWidth] = useState(0);
  const callback = useRef(onIssues); callback.current = onIssues;
  const normalFrame = useRef<HTMLIFrameElement>(null), fullFrame = useRef<HTMLIFrameElement>(null);
  const generation = useRef(0);
  const options = JSON.stringify(config || defaultDIYLandingOptions());
  const referenceID = reference?.page_id || '', revisionID = reference?.revision_id || '';
  const content = JSON.stringify(reference?.content || {});
  const large = (html?.length || 0) > 256 * 1024;
  const manualContent = useRef({ html, options, mode });
  const [manualTick, setManualTick] = useState(0);
  manualContent.current = { html, options, mode };
  const currentHTML = large ? undefined : html;

  useEffect(() => {
    const stage = normalStage.current;
    if (!stage) return;
    const measure = () => {
      const style = window.getComputedStyle(stage);
      const available = stage.clientWidth - parseFloat(style.paddingLeft || '0') - parseFloat(style.paddingRight || '0');
      if (available > 0) setStageWidth(available);
    };
    measure();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', measure);
      return () => window.removeEventListener('resize', measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(stage);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!result?.channel) return;
    const receive = (event: MessageEvent) => {
      if (event.source !== normalFrame.current?.contentWindow && event.source !== fullFrame.current?.contentWindow) return;
      const value = event.data;
      if (value?.type !== 'd1-diy' || value.channel !== result.channel || value.action !== 'diagnostic' || typeof value.message !== 'string') return;
      const message = value.message.replace(/\x00/g, '').slice(0, 200);
      if (!message) return;
      setError(message);
      callback.current?.([...(result.issues || []), { code: 'runtime', message, severity: 'error' }]);
    };
    window.addEventListener('message', receive);
    return () => window.removeEventListener('message', receive);
  }, [result]);

  useEffect(() => {
    const request = ++generation.current;
    const controller = new AbortController();
    const input = large ? manualContent.current : { html: currentHTML, options, mode };
    if (!input.html?.trim() && !referenceID) { setResult(undefined); setError(''); setBusy(false); callback.current?.(noIssues); return () => controller.abort(); }
    const timeout = setTimeout(async () => {
      setBusy(true); setError('');
      try {
        const body = referenceID && revisionID
          ? { reference: { page_id: referenceID, revision_id: revisionID, ...(content !== '{}' ? { content: JSON.parse(content) } : {}) }, mode: input.mode }
          : { html: input.html || '', config: JSON.parse(input.options) as DIYLandingOptions, mode: input.mode };
        const next = await api<PreviewResponse>('/diy-landings/preview', { method: 'POST', body: JSON.stringify(body), signal: controller.signal });
        if (generation.current !== request) return;
        setResult(next); callback.current?.(next.issues || noIssues);
      } catch (failure) {
        if (!controller.signal.aborted && generation.current === request) setError(errorText(failure));
      } finally { if (!controller.signal.aborted && generation.current === request) setBusy(false); }
    }, refresh || refreshToken || manualTick ? 0 : 400);
    return () => { clearTimeout(timeout); controller.abort(); };
  }, [currentHTML, options, mode, referenceID, revisionID, content, refresh, refreshToken, manualTick, large]);

  const frame = (expanded = false) => <div ref={expanded ? undefined : normalStage} className={`diy-preview-stage ${expanded && full === 'desktop' ? 'desktop' : 'phone'} ${expanded ? 'expanded' : ''}`}><div className="diy-preview-viewport" style={expanded && full === 'desktop' ? undefined : { width, maxWidth: 'none', zoom: !expanded && stageWidth > 0 ? Math.min(1, stageWidth / width) : 1 }}>
    {result ? <iframe ref={expanded ? fullFrame : normalFrame} title={expanded ? t('DIY落地页全屏预览') : t('DIY落地页实时预览')} sandbox="allow-scripts" referrerPolicy="no-referrer" src={result.preview_url || result.src_url} srcDoc={result.preview_url || result.src_url ? undefined : result.html} /> : <div className="diy-preview-empty"><Smartphone size={38} strokeWidth={1.2}/><strong>{t('粘贴代码，查看页面效果')}</strong><p>{t('代码只用于预览，保存并发布后才可在应用中选择。')}</p></div>}
  </div></div>;

  return <section className={`diy-preview ${compact ? 'compact' : ''}`} aria-label={t('落地页实时预览')}>
    <div className="diy-preview-toolbar"><div><strong>{t('实时预览')}</strong><span role="status">{busy ? t('更新中…') : error ? t('预览更新失败') : result ? t('预览已更新') : t('等待代码')}</span></div><div className="diy-preview-actions"><span className="diy-preview-device"><Smartphone size={15}/>{t('手机')}</span><select aria-label={t('手机预览宽度')} value={width} onChange={event => setWidth(Number(event.target.value))}><option value={375}>375 px</option><option value={390}>390 px</option><option value={430}>430 px</option></select><button type="button" className="diy-desktop-preview" disabled={!result} onClick={() => setFull('desktop')}><Monitor size={15}/>{t('桌面')}<Expand size={13}/></button><button type="button" className="icon-btn" aria-label={t('刷新预览')} onClick={() => { setRefresh(value => value + 1); setManualTick(value => value + 1); }}><RefreshCw size={16} className={busy ? 'spin' : ''}/></button><button type="button" className="icon-btn" aria-label={t('全屏预览')} disabled={!result} onClick={() => setFull('phone')}><Expand size={16}/></button></div></div>
    {large && <p className="diy-preview-note">{t('代码较大，修改后点击刷新预览。')}</p>}
    <FormError>{error}</FormError>{frame()}
    <p className="diy-preview-note">{t('预览不会安装、下载、跳转或产生计费。')}</p>
    {full && <Modal fullscreen title={`${full === 'desktop' ? t('桌面') : t('手机')} · ${t('全屏预览')}`} close={() => setFull(undefined)}>{frame(true)}</Modal>}
  </section>;
}
