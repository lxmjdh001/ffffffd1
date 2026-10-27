// i18n: static UI literals
import { t, useLocale } from './i18n';
import { LanguageSelector } from './language-selector';
import { useEffect, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useNotificationContext } from 'react-admin';
import { useQueryClient } from '@tanstack/react-query';
import { ArrowRight, ShieldCheck, Link2, CloudUpload, Eye, EyeOff, RefreshCw } from 'lucide-react';
import { api, authProvider } from './api';
import { Field, FormError, useConfig, errorText } from './ui';
import { Brand } from './brand';

function BotCheck({ action, refresh, onCaptcha }: { action: string; refresh: number; onCaptcha: (id: string, code: string) => void }) {
  useLocale();
  const [challenge, setChallenge] = useState<{ id: string; image: string }>(); const [code, setCode] = useState(''); const [error, setError] = useState(''); const [bump, setBump] = useState(0);
  useEffect(() => { let cancelled = false; setError(''); onCaptcha('', ''); setCode('');
    api<{ id: string; image: string }>('/auth/captcha').then(data => { if (!cancelled) { setChallenge(data); onCaptcha(data.id, ''); } }).catch(e => setError(errorText(e)));
    return () => { cancelled = true; };
    // Callbacks write state only; intentionally refresh on challenge identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [action, refresh, bump]);
  return <div className="bot-check">{<Field label={t("图形验证码")}><div className="captcha-row"><input required autoComplete="off" inputMode="numeric" maxLength={5} placeholder={t("输入图片中的数字")} value={code} onChange={e => { setCode(e.target.value); onCaptcha(challenge?.id || '', e.target.value); }} />{challenge && <button type="button" className="captcha-image" title={t("换一张验证码")} onClick={() => setBump(bump + 1)}><img src={challenge.image} alt={t("数字验证码，点击刷新")} /></button>}</div></Field>}{error && <><FormError>{error}</FormError><button type="button" className="text-btn" onClick={() => setBump(bump + 1)}><RefreshCw size={13} />{t("重新验证")}</button></>}</div>;
}
export { Brand } from './brand';

function readInviteCode(search: string) { return new URLSearchParams(search).get('invite') ?? new URLSearchParams(window.location.search).get('invite') ?? ''; }

export function AuthPage({ register = false }: { register?: boolean }) {
  useLocale();
  const location = useLocation();
  const navigate = useNavigate();
  const cache = useQueryClient();
  const { resetNotifications } = useNotificationContext();
  const [inviteCode, setInviteCode] = useState(() => readInviteCode(location.search));
  useEffect(() => { setInviteCode(readInviteCode(location.search)); }, [location.search]);
  const hasInviteQuery = new URLSearchParams(location.search).has('invite') || new URLSearchParams(window.location.search).has('invite');
  const inviteQuery = inviteCode.trim() || hasInviteQuery ? `?invite=${encodeURIComponent(inviteCode.trim())}` : '';
  const { config } = useConfig(); const [email, setEmail] = useState(''); const [name, setName] = useState(''); const [password, setPassword] = useState(''); const [show, setShow] = useState(false); const [captchaID, setCaptchaID] = useState(''); const [captchaCode, setCaptchaCode] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false); const [refresh, setRefresh] = useState(0);
  const submit = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setError('');
    const values = { email, name, password, captcha_id: captchaID, captcha_code: captchaCode, ...(register && inviteCode.trim() ? { invite_code: inviteCode.trim() } : {}) };
    try {
      const { user } = await authProvider.login({ ...values, action: register ? 'register' : 'login' });
      await cache.cancelQueries();
      cache.clear();
      // Registration lives inside the admin router, so its anonymous auth
      // result and any previous account's data must be replaced before entry.
      cache.setQueryData(['auth', 'checkAuth', {}], true);
      cache.setQueryData(['auth', 'getPermissions', {}], user.role);
      cache.setQueryData(['auth', 'getIdentity'], { ...user, fullName: user.name });
      resetNotifications();
      navigate('/', { replace: true });
    } catch (e) { setError(errorText(e)); setRefresh(n => n + 1); }
    finally { setBusy(false); }
  };
  return <div className="auth-page"><aside className="auth-story"><Link to={`/welcome${inviteQuery}`}><Brand dark /></Link><div className="auth-story-body"><div className="eyebrow">YOUR APPS. ONE LINK.</div><h1>{t("每一次发布，")}<br />{t("都更进一步。")}</h1><p>{t("从安装包到下载链接，")}<br />{t("让应用交付变得简单、有序。")}</p><div className="orbit-visual"><div className="orbit one" /><div className="orbit two" /><div className="orbit-center"><CloudUpload size={43} strokeWidth={1.3} /></div><span className="orbit-tag t1">.apk</span><span className="orbit-tag t2">.exe</span><span className="orbit-tag t3">.ipa</span><span className="orbit-link"><Link2 size={15} /> {t(" 一个链接，连接每位用户")}</span></div><div className="auth-features"><span><ShieldCheck size={16} />{t("安全验证")}</span><span><Link2 size={16} />{t("独立域名")}</span><span><CloudUpload size={16} />{t("多端分发")}</span></div></div><small>© {new Date().getFullYear()} {t(" D1 Apk · 应用分发平台")}</small></aside><main className="auth-main"><div className="auth-top"><LanguageSelector />{register ? t("已经有账号？") : t("还没有账号？")} <Link to={`${register ? '/login' : '/register'}${inviteQuery}`}>{register ? t("立即登录") : t("立即注册")} <ArrowRight size={14} /></Link></div><div className="auth-form"><span className="mini-label">{t("欢迎来到 D1 Apk")}</span><h2>{register ? t("创建你的账户") : t("登录控制台")}</h2><p>{register ? t("几分钟内，发布你的第一个应用。") : t("你的应用、用户与每一次交付，都在这里。")}</p>{config.development && <div className="notice compact">{t("本地开发环境 · 使用图形验证码验证")}</div>}<form onSubmit={submit}>{register && <Field label={t("用户名称")}><input required maxLength={40} value={name} onChange={e => setName(e.target.value)} placeholder={t("例如：星河工作室")} autoComplete="name" /></Field>}<Field label={register ? t("邮箱地址") : t("账号")}><input required type={register ? "email" : "text"} autoComplete="username" maxLength={254} value={email} onChange={e => setEmail(e.target.value)} placeholder={register ? "you@example.com" : t("邮箱或管理员账号")} /></Field><Field label={t("密码")} hint={register ? t("6–72 字节，建议组合字母、数字与符号") : undefined}><div className="password-input"><input required type={show ? 'text' : 'password'} autoComplete={register ? 'new-password' : 'current-password'} minLength={register ? 6 : 1} maxLength={72} value={password} onChange={e => setPassword(e.target.value)} placeholder={t("输入你的密码")} /><button type="button" onClick={() => setShow(!show)} aria-label={show ? t("隐藏密码") : t("显示密码")}>{show ? <EyeOff size={17} /> : <Eye size={17} />}</button></div></Field>{register && <Field label={t("邀请码（选填）")} hint={t("通过好友邀请注册，将建立长期邀请关系。没有邀请码可直接留空。")}><input aria-label={t("邀请码（选填）")} maxLength={64} value={inviteCode} onChange={e => setInviteCode(e.target.value)} placeholder={t("填写好友分享的邀请码")} autoComplete="off" spellCheck={false} /></Field>}<BotCheck action={register ? 'register' : 'login'} refresh={refresh} onCaptcha={(id, value) => { setCaptchaID(id); setCaptchaCode(value); }} /><FormError>{error}</FormError><button className="btn primary full" disabled={busy || (register && !config.registration_open)}>{busy ? t("正在处理…") : register ? config.registration_open ? t("创建账户") : t("暂未开放注册") : t("登 录")}<ArrowRight size={17} /></button></form><div className="auth-note"><ShieldCheck size={15} />{t("密码加密保存 · 安全会话登录")}</div>{!register && <p className="help-line">{t("忘记密码？请联系平台管理员协助重置。")}{config.support_email && <a href={`mailto:${config.support_email}`}>{config.support_email}</a>}</p>}</div></main></div>;
}
