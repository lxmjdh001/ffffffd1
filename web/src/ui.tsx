// i18n: static UI literals
import { getLocale, t, useLocale } from './i18n';
import { createContext, useContext, useEffect, useRef, type ReactNode } from 'react';
import { AlertCircle, Check, Copy, LoaderCircle, PackageOpen, X, ChevronLeft, ChevronRight } from 'lucide-react';
import { useNotify } from 'react-admin';
import type { Config } from './types';

export const ConfigContext = createContext<{ config: Config; reload: () => void }>(null!);
export const useConfig = () => useContext(ConfigContext);
export const money = (n: number, digits = 2) => (n / 1e6).toLocaleString(getLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
export const number = (n = 0) => n.toLocaleString(getLocale());
export const bytes = (n = 0) => n >= 2 ** 30 ? `${(n / 2 ** 30).toFixed(1)} GB` : n >= 2 ** 20 ? `${(n / 2 ** 20).toFixed(1)} MB` : `${(n / 1024).toFixed(1)} KB`;
export const date = (s?: string | null, full = false) => s ? new Intl.DateTimeFormat(getLocale(), { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', ...(full ? { hour: '2-digit', minute: '2-digit', second: '2-digit' } : {}) }).format(new Date(s)) : '—';
export const errorText = (e: unknown) => e instanceof Error ? t(e.message) : t("操作失败，请重试");
export const ledgerKind = (kind: string) => ({ return_flow: t('回流页扣费'), referral_reward: t('邀请返利'), withdrawal_hold: t('提现冻结'), withdrawal_refund: t('提现退回') } as Record<string, string>)[kind] || '';
export function PageHeader({ eyebrow, title, subtitle, action }: { eyebrow?: string; title: string; subtitle?: string; action?: ReactNode }) {
  useLocale(); return <div className="page-heading"><div>{eyebrow && <div className="eyebrow">{eyebrow}</div>}<h1>{title}</h1>{subtitle && <p>{subtitle}</p>}</div>{action && <div className="heading-action">{action}</div>}</div>; }
export function Spinner() {
  useLocale(); return <div className="loading"><LoaderCircle size={26} className="spin" /><span>{t("正在加载…")}</span></div>; }
export function ErrorState({ error, retry }: { error: unknown; retry?: () => void }) {
  useLocale(); return <div className="error-state"><AlertCircle size={25} /><p>{errorText(error)}</p>{retry && <button className="btn secondary" onClick={retry}>{t("重新加载")}</button>}</div>; }
export function Empty({ title = t("还没有记录"), detail, action }: { title?: string; detail?: string; action?: ReactNode }) {
  useLocale(); return <div className="empty"><div className="empty-icon"><PackageOpen size={30} strokeWidth={1.4} /></div><h3>{title}</h3>{detail && <p>{detail}</p>}{action}</div>; }
export function Badge({ children, tone = 'green' }: { children: ReactNode; tone?: string }) {
  useLocale(); return <span className={`badge ${tone}`}><i />{children}</span>; }
export function CopyButton({ value, label }: { value: string; label?: string }) {
  useLocale(); const notify = useNotify(); return <button className="copy-btn" title={label || t("复制")} onClick={async () => { try { await navigator.clipboard.writeText(value); notify(t("已复制"), { type: 'success' }); } catch { notify(t("复制失败，请手动选择复制"), { type: 'warning' }); } }}><Copy size={14} />{label}</button>; }
export function Modal({ title, children, close, wide = false, side = false }: { title: string; children: ReactNode; close: () => void; wide?: boolean; side?: boolean }) {
  useLocale();
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => { const dialog = ref.current!; dialog.showModal(); const old = document.body.style.overflow; document.body.style.overflow = 'hidden'; return () => { document.body.style.overflow = old; if (dialog.open) dialog.close(); }; }, []);
  return <dialog ref={ref} className={`modal ${wide ? 'wide' : ''} ${side ? 'side-drawer' : ''}`} onCancel={e => { e.preventDefault(); close(); }} onClick={e => { if (e.target === ref.current) close(); }}><div className="modal-inner"><div className="modal-head"><h2>{title}</h2><button className="icon-btn" aria-label={t("关闭")} onClick={close}><X size={20} /></button></div>{children}</div></dialog>;
}
export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  useLocale(); return <label className="field"><span>{label}</span>{children}{hint && <small>{hint}</small>}</label>; }
export function Pager({ page, total, setPage, size = 20 }: { page: number; total: number; setPage: (p: number) => void; size?: number }) {
  useLocale(); return <div className="pager"><span>{t("共 ")}{number(total)} {t(" 条记录")}</span><div><button className="icon-btn" disabled={page === 1} aria-label={t("上一页")} onClick={() => setPage(page - 1)}><ChevronLeft size={17} /></button><span>{page} / {Math.max(1, Math.ceil(total / size))}</span><button className="icon-btn" disabled={page * size >= total} aria-label={t("下一页")} onClick={() => setPage(page + 1)}><ChevronRight size={17} /></button></div></div>; }
export function AppIcon({ platform, small = false }: { platform: string; small?: boolean }) {
  useLocale(); const labels: Record<string, string> = { Android: 'A', iOS: 'i', Windows: 'W', macOS: 'M' }; return <span className={`app-icon ${platform.toLowerCase()} ${small ? 'small' : ''}`}>{labels[platform] || 'D'}</span>; }
export function CheckLine({ children }: { children: ReactNode }) {
  useLocale(); return <div className="check-line"><Check size={16} />{children}</div>; }
export function FormError({ children }: { children?: ReactNode }) {
  useLocale(); return children ? <div className="form-error" role="alert"><AlertCircle size={16} /><span>{typeof children === 'string' ? t(children) : children}</span></div> : null; }
