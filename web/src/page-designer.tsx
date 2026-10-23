import { useEffect, useRef, useState, type ChangeEvent } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, ExternalLink, ImagePlus, Monitor, RotateCcw, Smartphone, Trash2 } from 'lucide-react';
import { api, send } from './api';
import type { App } from './types';
import { useDownloadDomain } from './download-domain';
import { Badge, ErrorState, errorText, Field, FormError, PageHeader, Spinner, useConfig } from './ui';

export type PageDesign = { layout: 'google_play' | 'store' | 'minimal'; title: string; developer: string; summary: string; description: string; icon: string; screenshots: string[]; tags: string[]; button_text: string; theme_color: string; background_color: string; show_description: boolean; show_screenshots: boolean; show_file_info: boolean; show_downloads: boolean };
type DesignResponse = { app: App; design: PageDesign; defaults: PageDesign };

async function readPicture(file: File, icon = false): Promise<string> {
 if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) throw new Error('请选择 PNG、JPEG 或 WebP 图片');
 if (file.size > 8 * 1024 * 1024) throw new Error('单张原图不能超过 8 MB');
 const source = URL.createObjectURL(file);
 try {
  const image = new Image(); image.src = source; await image.decode();
  const ratio = Math.min(1, (icon ? 512 : 1400) / Math.max(image.width, image.height));
  const canvas = document.createElement('canvas'); canvas.width = Math.max(1, Math.round(image.width * ratio)); canvas.height = Math.max(1, Math.round(image.height * ratio));
  const context = canvas.getContext('2d'); if (!context) throw new Error('浏览器无法处理图片');
  context.fillStyle = '#ffffff'; context.fillRect(0, 0, canvas.width, canvas.height); context.drawImage(image, 0, 0, canvas.width, canvas.height);
  for (const quality of [0.85, 0.7, 0.5, 0.3]) { const output = canvas.toDataURL('image/jpeg', quality); if (output.length < 120000) return output; }
  throw new Error('图片压缩后仍过大，请选择更小或更简单的图片');
 } finally { URL.revokeObjectURL(source); }
}

export function PageDesigner() {
 const { id } = useParams(); const navigate = useNavigate(); const cache = useQueryClient(); const { config } = useConfig(); const { data: assigned } = useDownloadDomain();
 const { data, error, refetch } = useQuery({ queryKey: ['page-design', id], queryFn: () => api<DesignResponse>(`/apps/${id}/page-design`) });
 const [draft, setDraft] = useState<PageDesign>(); const [dirty, setDirty] = useState(false); const [busy, setBusy] = useState(false); const [imageBusy, setImageBusy] = useState(false); const [message, setMessage] = useState(''); const [saved, setSaved] = useState(false);
 const [html, setHTML] = useState(''); const [previewError, setPreviewError] = useState(''); const [previewBusy, setPreviewBusy] = useState(false); const [desktop, setDesktop] = useState(false); const revision = useRef(0);
 useEffect(() => { setDraft(undefined); setDirty(false); }, [id]);
 useEffect(() => { if (data) setDraft(previous => previous || structuredClone(data.design)); }, [data]);
 useEffect(() => {
  if (!draft) return; const version = ++revision.current; const controller = new AbortController();
  const timeout = window.setTimeout(async () => { setPreviewBusy(true); try {
   const out = await api<{ html: string }>(`/apps/${id}/page-design/preview`, { method: 'POST', body: JSON.stringify(draft), signal: controller.signal });
   if (version === revision.current && !controller.signal.aborted) { setHTML(out.html); setPreviewError(''); }
  } catch (e) { if (!controller.signal.aborted && version === revision.current) setPreviewError(errorText(e)); }
  finally { if (!controller.signal.aborted && version === revision.current) setPreviewBusy(false); } }, 350);
  return () => { window.clearTimeout(timeout); controller.abort(); };
 }, [draft, id]);
 const update = <K extends keyof PageDesign>(key: K, value: PageDesign[K]) => { setDraft(previous => previous ? { ...previous, [key]: value } : previous); setDirty(true); setSaved(false); setMessage(''); };
 const picture = async (e: ChangeEvent<HTMLInputElement>, icon: boolean) => {
  const files = Array.from(e.target.files || []); e.target.value = ''; if (!files.length || !draft) return;
  if (!icon && files.length + draft.screenshots.length > 5) { setMessage('最多添加 5 张截图'); return; }
  setImageBusy(true); setMessage(''); try { const values = await Promise.all(files.map(f => readPicture(f, icon))); if (icon) update('icon', values[0]); else update('screenshots', [...draft.screenshots, ...values]); } catch (e) { setMessage(errorText(e)); } finally { setImageBusy(false); }
 };
 const save = async () => {
  if (!draft) return; setBusy(true); setMessage(''); setSaved(false);
  try { await send(`/apps/${id}/page-design`, draft, 'PUT'); cache.setQueryData(['page-design', id], { ...data, design: draft }); setDirty(false); setSaved(true); navigate('/apps'); } catch (e) { setMessage(errorText(e)); } finally { setBusy(false); }
 };
 if (error) return <ErrorState error={error} retry={() => refetch()} />;
 if (!data || !draft) return <Spinner />;
 const base = assigned?.download_base_url || (config.development ? config.public_url : '');
 const moveScreenshot = (index: number, offset: number) => { const next = [...draft.screenshots]; [next[index], next[index + offset]] = [next[index + offset], next[index]]; update('screenshots', next); };
 return <div className="page-designer"><Link to={`/apps/${id}`} className="back-link"><ArrowLeft size={15} />返回应用管理</Link>
 <PageHeader eyebrow="PAGE DESIGNER" title="详情页设计" subtitle={`${data.app.name} · 编辑后保存，即时更新公开详情页。`} action={<Badge tone={dirty ? 'amber' : 'green'}>{dirty ? '有未保存的修改' : '已同步'}</Badge>} />
 <div className={`designer-grid ${desktop ? 'desktop-preview' : ''}`}><div className="designer-form"><fieldset disabled={busy || imageBusy}>
 <section className="panel padded"><h2>样式与布局</h2><div className="form-grid"><Field label="页面模板"><select value={draft.layout} onChange={e => update('layout', e.target.value as PageDesign['layout'])}><option value="google_play">Google Play</option><option value="store">应用商店</option><option value="minimal">简洁下载页</option></select></Field><Field label="下载按钮文字"><input required maxLength={30} value={draft.button_text} onChange={e => update('button_text', e.target.value)} /></Field><Field label="主题色"><div className="designer-color"><input type="color" aria-label="主题颜色" value={draft.theme_color} onChange={e => update('theme_color', e.target.value)} /><code>{draft.theme_color}</code></div></Field><Field label="背景色"><div className="designer-color"><input type="color" aria-label="背景颜色" value={draft.background_color} onChange={e => update('background_color', e.target.value)} /><code>{draft.background_color}</code></div></Field></div></section>
 <section className="panel padded"><h2>应用展示信息</h2><p className="muted small">用于详情页展示，不改变安装包和原有下载链接。</p><div className="form-grid"><Field label="展示名称"><input required maxLength={100} value={draft.title} onChange={e => update('title', e.target.value)} /></Field><Field label="开发者 / 品牌"><input maxLength={80} value={draft.developer} onChange={e => update('developer', e.target.value)} placeholder="填写真实的发布者名称" /></Field></div>
 <Field label="应用图标"><div className="designer-icon-row">{draft.icon ? <img src={draft.icon} alt="应用图标" /> : <div className="designer-icon-placeholder"><ImagePlus size={26} /></div>}<label className="btn secondary">上传图标<input type="file" hidden accept="image/png,image/jpeg,image/webp" onChange={e => picture(e, true)} /></label>{draft.icon && <button className="text-btn danger-text" onClick={() => update('icon', '')}>移除</button>}</div></Field><Field label="或填写图标 HTTPS 地址"><input type="url" value={draft.icon.startsWith('data:') ? '' : draft.icon} placeholder="https://example.com/icon.png" onChange={e => update('icon', e.target.value)} /></Field>
 <Field label="一句话简介"><textarea rows={2} maxLength={200} value={draft.summary} onChange={e => update('summary', e.target.value)} placeholder="介绍应用的核心用途" /></Field><Field label="应用介绍"><textarea rows={5} maxLength={4000} value={draft.description} onChange={e => update('description', e.target.value)} /></Field><Field label="标签" hint="以逗号分隔，最多 6 个，每个不超过 20 字。"><input value={draft.tags.join(',')} onChange={e => update('tags', e.target.value.split(/[,，]/))} placeholder="工具,效率,生活" /></Field></section>
 <section className="panel padded"><h2>应用截图</h2><p className="muted small">最多 5 张，上传后自动压缩。支持调整顺序。</p><div className="designer-screenshots">{draft.screenshots.map((src, i) => <div className="designer-shot" key={i}><img src={src} alt={`截图 ${i + 1}`} /><div><button className="text-btn" aria-label={`前移截图 ${i+1}`} disabled={i === 0} onClick={() => moveScreenshot(i, -1)}>←</button><button className="icon-btn danger-text" aria-label={`移除截图 ${i+1}`} onClick={() => update('screenshots', draft.screenshots.filter((_, j) => i !== j))}><Trash2 size={14} /></button><button className="text-btn" aria-label={`后移截图 ${i+1}`} disabled={i === draft.screenshots.length-1} onClick={() => moveScreenshot(i, 1)}>→</button></div></div>)}</div>{draft.screenshots.length < 5 && <label className="btn secondary"><ImagePlus size={15} />上传截图<input type="file" hidden multiple accept="image/png,image/jpeg,image/webp" onChange={e => picture(e, false)} /></label>}
 <Field label="或添加 HTTPS 截图地址（回车添加）"><input type="url" placeholder="https://example.com/screenshot.jpg" onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); const value = e.currentTarget.value.trim(); if (value && draft.screenshots.length < 5) { update('screenshots', [...draft.screenshots, value]); e.currentTarget.value = ''; } } }} /></Field></section>
 <section className="panel padded"><h2>内容显示</h2>{([['show_description', '显示应用介绍'], ['show_screenshots', '显示应用截图'], ['show_file_info', '显示文件信息'], ['show_downloads', '显示真实下载次数']] as const).map(([key, label]) => <label className="switch-field" key={key}><input type="checkbox" checked={draft[key]} onChange={e => update(key, e.target.checked)} /><span>{label}</span></label>)}</section>
 </fieldset><div className="designer-save panel padded"><div className="row-actions wrap"><button className="btn primary" disabled={busy || imageBusy} onClick={save}><Check size={15} />{busy ? '保存中…' : '保存并发布'}</button><button className="btn secondary" disabled={busy || imageBusy} onClick={() => { setDraft(structuredClone(data.defaults)); setDirty(true); setSaved(false); setMessage(''); }}><RotateCcw size={15} />恢复默认</button>{base && <a className="text-btn" href={`${base}/a/${data.app.slug}/info`} target="_blank" rel="noreferrer">查看已发布页面<ExternalLink size={13} /></a>}</div><FormError>{message}</FormError>{imageBusy && <p className="small muted">正在处理图片…</p>}{saved && <p className="success-line" role="status">已保存，公开详情页已更新</p>}</div></div>
 <aside className="designer-preview"><div className="designer-preview-toolbar"><strong>实时预览</strong><span className="muted small">{previewBusy ? '更新中…' : dirty ? '未发布预览' : '当前效果'}</span><div><button className={`btn ${!desktop ? 'primary' : 'secondary'}`} onClick={() => setDesktop(false)}><Smartphone size={15} />手机</button><button className={`btn ${desktop ? 'primary' : 'secondary'}`} onClick={() => setDesktop(true)}><Monitor size={15} />桌面</button></div></div><div className={`designer-device ${desktop ? 'desktop' : 'phone'}`}><iframe title="应用详情页实时预览" srcDoc={html} sandbox="" referrerPolicy="no-referrer" /></div><FormError>{previewError}</FormError><p className="muted small">预览中的下载按钮不可点击，保存后在公开页面测试下载。</p></aside></div></div>;
}
