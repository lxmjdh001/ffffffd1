import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown, Languages } from 'lucide-react';
import { languageOptions, setLocale, t, useLocale, type Locale } from './i18n';
import './language-selector.css';

export function LanguageSelector() {
  const locale = useLocale();
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<{ top: number; left: number; maxHeight: number }>();
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const items = useRef<(HTMLButtonElement | null)[]>([]);
  const pendingFocus = useRef(0);
  const id = useId();
  const current = languageOptions.find(option => option.value === locale)!;
  const badge = (value: Locale) => value === 'zh-CN' ? '简' : value === 'zh-TW' ? '繁' : value.toUpperCase();
  const close = (restoreFocus = false) => { setOpen(false); if (restoreFocus) trigger.current?.focus(); };
  const show = (index = languageOptions.findIndex(option => option.value === locale)) => {
    if (open) { items.current[index]?.focus(); return; }
    pendingFocus.current = index;
    setPosition(undefined);
    setOpen(true);
  };
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const anchor = trigger.current?.getBoundingClientRect();
      if (!anchor || !menu.current) return;
      if (anchor.bottom < 0 || anchor.top > window.innerHeight) { setOpen(false); return; }
      const width = Math.min(308, window.innerWidth - 24);
      const height = Math.min(menu.current.scrollHeight, 320);
      const below = window.innerHeight - anchor.bottom - 20;
      const above = anchor.top - 20;
      const upwards = below < height && above > below;
      const maxHeight = Math.max(0, upwards ? above : below);
      setPosition({ left: Math.max(12, Math.min(anchor.right - width, window.innerWidth - width - 12)), top: upwards ? Math.max(12, anchor.top - Math.min(height, maxHeight) - 8) : anchor.bottom + 8, maxHeight });
    };
    place();
    window.addEventListener('resize', place);
    // Capture scrolling ancestors too, so the floating card stays anchored.
    window.addEventListener('scroll', place, true);
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
  }, [open]);
  useLayoutEffect(() => {
    if (open && position && pendingFocus.current >= 0) {
      items.current[pendingFocus.current]?.focus({ preventScroll: true });
      pendingFocus.current = -1;
    }
  }, [open, position]);
  useEffect(() => {
    if (!open) return;
    const dismiss = (event: PointerEvent) => {
      if (!trigger.current?.contains(event.target as Node) && !menu.current?.contains(event.target as Node)) close();
    };
    document.addEventListener('pointerdown', dismiss);
    return () => document.removeEventListener('pointerdown', dismiss);
  }, [open]);
  const choose = (value: Locale) => { setLocale(value); close(true); };
  const navigate = (event: KeyboardEvent<HTMLDivElement>) => {
    const index = items.current.indexOf(document.activeElement as HTMLButtonElement);
    let next: number | undefined;
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true); return; }
    if (event.key === 'Tab') { close(true); return; }
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = languageOptions.length - 1;
    if (event.key === 'ArrowDown' || event.key === 'ArrowRight') next = (index + 1) % languageOptions.length;
    if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') next = (index - 1 + languageOptions.length) % languageOptions.length;
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      next = languageOptions.findIndex((option, i) => i > index && option.label.toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()));
      if (next < 0) next = languageOptions.findIndex(option => option.label.toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()));
    }
    if (next !== undefined && next >= 0) { event.preventDefault(); items.current[next]?.focus(); }
  };
  return <div className="language-switcher">
    <button ref={trigger} type="button" id={`${id}-trigger`} className="language-trigger" aria-label={t('语言')} aria-describedby={`${id}-current`} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? `${id}-menu` : undefined} title={current.label} onClick={() => open ? close() : show()} onKeyDown={event => {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); show(event.key === 'ArrowDown' ? 0 : languageOptions.length - 1); }
    }}><span className="language-trigger-icon"><Languages size={15} aria-hidden="true" /></span><span id={`${id}-current`} className="language-trigger-label" lang={locale}>{current.label}</span><ChevronDown className="language-trigger-chevron" size={12} aria-hidden="true" /></button>
    {open && createPortal(<div ref={menu} id={`${id}-menu`} className="language-menu" role="menu" aria-labelledby={`${id}-trigger`} style={{ top: position?.top ?? 0, left: position?.left ?? 0, maxHeight: position?.maxHeight, visibility: position ? 'visible' : 'hidden' }} onKeyDown={navigate}>
      <div className="language-menu-heading" aria-hidden="true"><Languages size={15} /><span>{t('语言')}</span><small>{badge(locale)}</small></div>
      <div className="language-menu-grid" role="none">{languageOptions.map((option, index) => <button ref={node => { items.current[index] = node; }} type="button" role="menuitemradio" aria-checked={locale === option.value} key={option.value} tabIndex={-1} className="language-option" onClick={() => choose(option.value)}><span className="language-option-code" aria-hidden="true">{badge(option.value)}</span><span lang={option.value}>{option.label}</span>{locale === option.value && <Check size={13} aria-hidden="true" />}</button>)}</div>
    </div>, document.body)}
  </div>;
}
