import { useEffect, useId, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowUpRight, Menu, X } from 'lucide-react';
import { Brand } from './auth';
import { t, useLocale } from './i18n';
import { LanguageSelector } from './language-selector';
import { useConfig } from './ui';
import { HomeContact } from './home-contact';

const sectionIDs = ['home-solutions', 'home-growth', 'home-pricing', 'home-faq'];

export function scrollToHomeSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' });
}

export function HomeNavigation({ inviteQuery, startPath }: { inviteQuery: string; startPath: string }) {
  useLocale();
  const { config } = useConfig();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState('');
  const menu = useRef<HTMLDivElement>(null);
  const toggle = useRef<HTMLButtonElement>(null);
  const panelID = useId();
  const startLabel = config.registration_open ? t('免费开始') : t('进入控制台');
  const sections = [
    { id: 'home-solutions', label: t('产品') },
    { id: 'home-growth', label: t('产品功能') },
    { id: 'home-pricing', label: t('定价') },
    { id: 'home-faq', label: t('常见问题') },
  ];
  useEffect(() => {
    let frame = 0;
    const update = () => {
      frame = 0;
      const top = (document.querySelector('.home-header')?.getBoundingClientRect().height || 66) + 32;
      let current = '';
      for (const id of sectionIDs) {
        if ((document.getElementById(id)?.getBoundingClientRect().top ?? Infinity) <= top) current = id;
      }
      setActive(current);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    update();
    window.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', schedule);
    return () => { cancelAnimationFrame(frame); window.removeEventListener('scroll', schedule); window.removeEventListener('resize', schedule); };
  }, []);
  useEffect(() => {
    if (!open) return;
    const dismiss = (event: PointerEvent) => { if (!menu.current?.contains(event.target as Node)) setOpen(false); };
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { setOpen(false); toggle.current?.focus(); }
    };
    const desktop = window.matchMedia?.('(min-width: 1200px)');
    const resize = () => { if (desktop?.matches) setOpen(false); };
    document.addEventListener('pointerdown', dismiss);
    document.addEventListener('keydown', escape);
    desktop?.addEventListener('change', resize);
    return () => {
      document.removeEventListener('pointerdown', dismiss);
      document.removeEventListener('keydown', escape);
      desktop?.removeEventListener('change', resize);
    };
  }, [open]);
  const visitSection = (id: string) => {
    setOpen(false);
    scrollToHomeSection(id);
  };
  const sectionButtons = () => sections.map(section => <button key={section.id} title={section.label} data-section={section.id} aria-current={active === section.id ? 'location' : undefined} onClick={() => visitSection(section.id)}>{section.label}</button>);
  const startLink = () => <Link className="home-button home-button-small" title={startLabel} to={startPath} onClick={() => setOpen(false)}><span>{startLabel}</span><ArrowUpRight size={15} /></Link>;
  return <header className="home-header"><nav className="home-width home-nav" aria-label={t('首页导航')}>
    <Link className="home-nav-brand" to={`/welcome${inviteQuery}`} aria-label={t('{0} 首页', { 0: config.site_name })} onClick={() => scrollToHomeSection('home-top')}><Brand /></Link>
    <div className="home-nav-sections">{sectionButtons()}</div>
    <div className="home-nav-actions"><LanguageSelector /><Link className="home-login" title={t('登录')} to={`/login${inviteQuery}`}>{t('登录')}</Link>{startLink()}</div>
    <div className="home-nav-menu" ref={menu}>
      <button ref={toggle} className="home-menu-toggle" aria-label={open ? t('关闭导航') : t('打开导航')} aria-expanded={open} aria-controls={panelID} onClick={() => setOpen(value => !value)}>{open ? <X size={20} /> : <Menu size={20} />}</button>
      {open && <div id={panelID} className="home-menu-panel"><div className="home-menu-sections">{sectionButtons()}</div><div className="home-menu-auth"><Link className="home-login" to={`/login${inviteQuery}`} onClick={() => setOpen(false)}>{t('登录')}</Link>{startLink()}<HomeContact onClick={() => setOpen(false)}/></div></div>}
    </div>
  </nav></header>;
}
