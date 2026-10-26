// i18n: static UI literals
import { t, useLocale } from './i18n';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { NavLink, Link, useLocation } from 'react-router-dom';
import { Notification, useGetIdentity, useLogout, usePermissions } from 'react-admin';
import { Boxes, Globe2, Wallet, ChartNoAxesCombined, Settings2, Users, ShieldCheck, ScrollText, SlidersHorizontal, LogOut, ArrowUpRight, Menu, X, CircleHelp, ChevronRight, CreditCard, Sparkles, Radio, Gift } from 'lucide-react';
import { Brand } from './auth';
import { useConfig } from './ui';
import './workspace-navigation.css';
import './console-design.css';
import { LanguageSelector } from './language-selector';

const navItems = [{ to: '/apps', get label() { return t("应用管理"); }, icon: Boxes }, { to: '/pwas', get label() { return t("PWA管理"); }, icon: Globe2 }, { to: '/pwa-push', get label() { return t("通知推送"); }, icon: Radio }, { to: '/domains', get label() { return t("域名管理"); }, icon: Globe2 }, { to: '/traffic', get label() { return t("应用分流"); }, icon: Globe2 }, { to: '/cloaks', get label() { return t("斗篷管理"); }, icon: ShieldCheck }, { to: '/billing', get label() { return t("会员与账单"); }, icon: Wallet }, { to: '/integrations', get label() { return t("广告回传"); }, icon: Radio }, { to: '/referrals', get label() { return t("邀请好友"); }, icon: Gift }, { to: '/activity', get label() { return t("下载记录"); }, icon: ChartNoAxesCombined }, { to: '/account', get label() { return t("账户设置"); }, icon: Settings2 }];
const adminItems = [{ to: '/admin/users', get label() { return t("用户管理"); }, icon: Users }, { to: '/admin/apps', get label() { return t("应用审核"); }, icon: ShieldCheck }, { to: '/admin/orders', get label() { return t("充值订单"); }, icon: CreditCard }, { to: '/admin/commissions', get label() { return t("佣金设置"); }, icon: Gift }, { to: '/admin/referral-withdrawals', get label() { return t("提现审核"); }, icon: Wallet }, { to: '/admin/settings', get label() { return t("平台设置"); }, icon: SlidersHorizontal }, { to: '/admin/audit', get label() { return t("操作日志"); }, icon: ScrollText }];
export function AppLayout({ children }: { children: ReactNode }) {
  useLocale();
  const { data: identity } = useGetIdentity(); const { permissions } = usePermissions(); const logout = useLogout(); const { config } = useConfig(); const [open, setOpen] = useState(false); const location = useLocation(); const current = location.pathname === '/' ? t("工作台") : [...navItems, ...adminItems].find(x => x.to === location.pathname || (x.to !== '/' && location.pathname.startsWith(x.to)))?.label || t("应用详情");
  const sidebarRef = useRef<HTMLElement>(null);
  const menuRef = useRef<HTMLButtonElement>(null);
  const [mobile, setMobile] = useState(() => window.matchMedia?.('(max-width: 650px)').matches ?? false);
  useEffect(() => {
    const query = window.matchMedia?.('(max-width: 650px)');
    if (!query) return;
    const update = () => { setMobile(query.matches); if (!query.matches) setOpen(false); };
    update();
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);
  useEffect(() => {
    if (!mobile || !open) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const focusable = () => Array.from(sidebarRef.current?.querySelectorAll<HTMLElement>('a[href], button:not(:disabled)') ?? []);
    focusable()[0]?.focus();
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); setOpen(false); }
      if (event.key === 'Tab') {
        const items = focusable();
        const first = items[0]; const last = items[items.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
      }
    };
    document.addEventListener('keydown', handleKey);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener('keydown', handleKey);
      menuRef.current?.focus();
    };
  }, [mobile, open]);
  return <div className="shell">{open && <button className="sidebar-backdrop" aria-label={t("关闭导航")} onClick={() => setOpen(false)} />}<aside ref={sidebarRef} id="workspace-sidebar" inert={mobile && !open} className={`sidebar ${open ? 'open' : ''}`}>{mobile && open && <button className="sidebar-close icon-btn" onClick={() => setOpen(false)} aria-label={t("关闭导航")}><X size={20} /></button>}<Link to="/" className="sidebar-brand" onClick={() => setOpen(false)}><Brand dark /><small>{t("应用分发控制台")}</small></Link><NavLink to="/" end aria-label={t("我的工作空间")} onClick={() => setOpen(false)} className={({ isActive }) => isActive ? 'workspace-label active' : 'workspace-label'}><span className="workspace-mark">D</span><div>{t("我的工作空间")}<small>WORKSPACE</small></div><ChevronRight size={14} /></NavLink><nav>{navItems.map(item => <NavLink key={item.to} to={item.to} end={item.to === '/'} onClick={() => setOpen(false)} className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'}><item.icon size={18} strokeWidth={1.7} /><span>{config.metered_billing && item.to === "/billing" ? t("用量与账单") : item.label}</span>{item.to === '/apps' && <span className="nav-dot" />}</NavLink>)}</nav>{permissions === 'admin' && <><div className="nav-label">{t("平台管理 ")}<span>ADMIN</span></div><nav>{adminItems.map(item => <NavLink key={item.to} to={item.to} onClick={() => setOpen(false)} className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'}><item.icon size={17} strokeWidth={1.7} /><span>{config.metered_billing && item.to === "/billing" ? t("用量与账单") : item.label}</span></NavLink>)}</nav></>}<div className="sidebar-bottom"><div className="upgrade-box"><Sparkles size={18} /><strong>{t("为更大的发布而准备")}</strong><p>{config.metered_billing ? t("仅 APK 下载与 PWA 安装按量计费。") : t("尊享套餐，让分发从容扩展。")}</p><Link to="/billing">{config.metered_billing ? t("查看用量与账单") : t("查看会员方案 ")}<ArrowUpRight size={15} /></Link></div><button className="sidebar-user" onClick={() => logout()} title={t("退出登录")}><span className="avatar">{identity?.fullName?.slice(0, 1) || 'D'}</span><span>{identity?.fullName || t("我的账户")}<small>{permissions === 'admin' ? t("平台管理员") : t("工作空间成员")}</small></span><LogOut size={16} /></button></div></aside><div className="main-shell" inert={mobile && open}><header className="topbar"><div><button ref={menuRef} className="mobile-menu icon-btn" onClick={() => setOpen(!open)} aria-controls="workspace-sidebar" aria-expanded={open} aria-label={open ? t("关闭导航") : t("打开导航")}>{open ? <X size={20} /> : <Menu size={20} />}</button><span className="breadcrumb"><span className="breadcrumb-root">{t("工作空间 ")}</span><ChevronRight size={13} /> <strong>{config.metered_billing && location.pathname === "/billing" ? t("用量与账单") : t(current)}</strong></span></div><div className="topbar-right"><LanguageSelector />{config.development && <span className="dev-tag">{t("本地开发")}</span>}<Link to="/help" aria-label={t("使用指南")} className="help-link"><CircleHelp size={17} /><span>{t("使用指南")}</span></Link><div className="topbar-divider" /><Link to="/account" aria-label={t("账户设置")} className="avatar small">{identity?.fullName?.slice(0, 1) || 'D'}</Link></div></header><main className="page-content">{children}</main><footer className="app-footer"><span>© {new Date().getFullYear()} {config.site_name}</span><span>{t("让每一次交付，都简单一点。")}</span></footer></div><Notification /></div>;
}
