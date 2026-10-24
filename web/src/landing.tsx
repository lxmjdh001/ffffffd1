import { UsageRates } from './usage-billing';
import { Link, useLocation } from 'react-router-dom';
import { ArrowDownRight, ArrowRight, ArrowUpRight, Bell, Check, Globe2, Layers3, Link2, ListFilter, Monitor, Package, Radio, RotateCcw, ShieldCheck, Smartphone } from 'lucide-react';
import { Brand } from './auth';
import { localizedPlanName, t, translateContent, useLocale } from './i18n';
import { bytes, money, number, useConfig } from './ui';
import originalHomeContent from './home-content.json';
import homeMedia from './home-media.json';
import { HomeNavigation } from './home-navigation';
import './landing.css';

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
}
const featureIcons = { security: ShieldCheck, domains: Globe2, analytics: Monitor, apk: Package, pwa: Smartphone, return: RotateCcw, routing: Link2, cloak: ListFilter, landing: Monitor, ads: Radio };

export function LandingPage() {
  useLocale();
  const content = translateContent(originalHomeContent);
  const { config } = useConfig();
  const location = useLocation();
  const invite = new URLSearchParams(location.search).get('invite') ?? new URLSearchParams(window.location.search).get('invite');
  const inviteQuery = invite === null ? '' : `?invite=${encodeURIComponent(invite.trim())}`;
  const startPath = `${config.registration_open ? '/register' : '/login'}${inviteQuery}`;
  const startLabel = config.registration_open ? t('免费开始') : t('登录控制台');
  return <div className="home home-editorial">
    <HomeNavigation inviteQuery={inviteQuery} startPath={startPath} />
    <main>
      <section className="home-width home-hero">
        <div className="home-edition"><span>D1 / DIGITAL PUBLISHING</span><span>{content.hero.kicker}</span><span>VOL. 01 — THE CONNECTION ISSUE</span></div>
        <div className="home-hero-layout"><div className="home-hero-copy"><h1>{content.hero.lines[0]}<br /><span>{content.hero.lines[1]}</span></h1><div className="home-hero-aside"><p>{content.hero.description}</p><div className="home-hero-actions"><Link className="home-button" to={startPath}>{config.registration_open ? t('免费创建账户') : t('登录控制台')}<ArrowUpRight size={17} /></Link><button className="home-text-button" onClick={() => scrollToSection('home-solutions')} aria-label={t('看看如何开始')}><ArrowDownRight size={24} /></button></div><small>{config.registration_open ? t('免费开启你的第一个应用 · 随时按需升级') : t('注册暂未开放 · 已有账户可登录')}</small></div></div><figure className="home-phone-visual"><img className="home-phone-image" src={homeMedia.phoneImage} alt={t('APK 下载、PWA 安装与安全检测的卡通手机示意')} width="1280" height="1280" fetchPriority="high" /><figcaption>APK / PWA / CONNECT</figcaption></figure></div>
        <div className="home-capability-strip"><button onClick={() => scrollToSection('home-security')}><ShieldCheck size={19}/><span>{t('自动检查域名安全')}</span><ArrowDownRight size={15}/></button><span><RotateCcw size={17}/>{t('回流页')}</span><span><Radio size={17}/>{t('广告回传')}</span><span><Monitor size={17}/>{t('下载统计')}</span></div>
      </section>

      <section className="home-width home-solutions" id="home-solutions" aria-labelledby="home-solutions-heading">
        <div className="home-section-top"><span className="home-eyebrow">01 / THE POSSIBILITIES</span><div><h2 id="home-solutions-heading">{content.solutions.heading}</h2><p>{content.solutions.intro}</p></div></div>
        <div className="home-stories">
          <article className="home-story home-story-apk"><div className="home-story-art apk-art" aria-hidden="true"><span className="art-note">D1 / RELEASE ARCHIVE</span><div className="apk-sheet sheet-back" /><div className="apk-sheet"><Package size={36} strokeWidth={1} /><span>APPLICATION</span><strong>.apk</strong><div><span>VERSION 1.0</span><ArrowDownRight size={20} /></div></div><span className="art-footnote">READY TO DISTRIBUTE</span></div><div className="home-story-heading"><span>01 — APK</span><ArrowUpRight size={22} /></div><h3>{t('上传你的应用')}</h3><p>{content.paths.apkDescription}</p><ul><li>APK · IPA</li><li>{t('版本管理')}</li><li>{t('下载统计')}</li></ul><Link to={startPath}>{startLabel}<ArrowRight size={16} /></Link></article>
          <article className="home-story home-story-pwa"><div className="home-story-art pwa-art" aria-hidden="true"><span className="art-note">D1 / WEB TO APP</span><div className="pwa-window"><div><i /><i /><i /></div><Globe2 size={48} strokeWidth={.8} /><span>YOUR WEBSITE</span></div><div className="pwa-install"><Smartphone size={20} /><span>ADD TO HOME</span><ArrowUpRight size={16} /></div><span className="art-footnote">A PLACE ON THE HOME SCREEN</span></div><div className="home-story-heading"><span>02 — PWA</span><ArrowUpRight size={22} /></div><h3>{t('让网页，走进用户的桌面')}</h3><p>{t('把网页变成可安装的应用，让用户从桌面再次找到你。')}</p><ul><li>{t('无需下载 APK')}</li><li>{t('独立桌面入口')}</li><li>Web Push</li></ul><Link to={startPath}>{startLabel}<ArrowRight size={16} /></Link></article>

        </div>
      </section>

      <section className="home-width home-connection" aria-labelledby="home-connection-heading"><div className="home-connection-title"><span className="home-eyebrow">02 / STAY IN THE STORY</span><h2 id="home-connection-heading">{t('发布之后，')}<br /><em>{t('连接继续。')}</em></h2><span className="home-large-asterisk" aria-hidden="true">✳</span></div><div className="home-connection-notes"><article className="home-pwa-explainer"><Smartphone size={22} strokeWidth={1.3} /><h3>{content.paths.pwaHeading}</h3><p>{content.paths.pwaDescription}</p><small>{t('实际安装方式取决于用户的浏览器和设备。')}</small></article><article className="home-push-copy"><Bell size={22} strokeWidth={1.3} /><h3>{content.push.heading}</h3><p>{content.push.description}</p><small>WEB PUSH / OPT-IN</small></article><article><RotateCcw size={22} strokeWidth={1.3} /><h3>{t('为返回，留一个新的入口。')}</h3><p>{t('图片、视频与自定义按钮，让回到页面的用户继续浏览、下载或安装。触发与展示频率由你设置。')}</p><small>RETURN PAGE / YOUR CONTENT</small></article></div></section>

      <section className="home-width home-features-wrap" id="home-features"><div className="home-section-top"><span className="home-eyebrow">03 / THE TOOLKIT</span><div><h2>{t('从容发布，细节有序。')}</h2><p>{t('在同一个工作空间，管理链接、页面与每一次有效连接。')}</p></div></div><div className="home-solution-grid">{content.solutions.items.filter(item => !['apk','pwa'].includes(item.id)).map((item,index) => { const Icon = featureIcons[item.id as keyof typeof featureIcons] || Layers3; return <article key={item.id} id={item.id === 'security' ? 'home-security' : undefined} className={item.id === 'security' ? 'home-security-feature' : item.id === 'return' ? 'home-return-feature' : undefined}><div className="home-solution-top"><span>0{index+1} / {item.label}</span><Icon size={20} strokeWidth={1.3} /></div><h3>{item.title}</h3><p>{item.description}</p><ul>{item.tags.map(tag => <li key={tag}>{tag}</li>)}</ul></article>; })}</div></section>

      <section className="home-width home-how" id="home-how" aria-labelledby="home-how-heading"><div className="home-section-top"><span className="home-eyebrow">04 / A SIMPLE START</span><div><h2 id="home-how-heading">{content.journey.heading}</h2></div></div><div className="home-steps">{content.journey.steps.map((step,index) => <article key={index}><span>0{index+1}</span><h3>{step.title}</h3><p>{step.description}</p></article>)}</div></section>

      {config.metered_billing && <section className="home-width home-pricing" id="home-pricing" aria-labelledby="home-pricing-heading"><div className="home-section-top home-pricing-copy"><span className="home-eyebrow">05 / PAY AS YOU GO</span><div><h2 id="home-pricing-heading">{t('全部功能免费开放')}<br/><em>{t('只为实际用量付费。')}</em></h2><p>{t('APK 访问与 PWA 安装共用 {0} 次赠送额度。',{0:number(config.registration_gift_limit ?? 1000)})}</p><Link className="home-button" to={startPath}>{startLabel}<ArrowUpRight size={17}/></Link></div></div><UsageRates config={config}/></section>}
{!config.metered_billing && <>      <section className="home-width home-pricing" id="home-pricing" aria-labelledby="home-pricing-heading"><div className="home-section-top home-pricing-copy"><span className="home-eyebrow">05 / FIND YOUR PACE</span><div><h2 id="home-pricing-heading">{t('从免费开始，')}<br /><em>{t('随成长升级。')}</em></h2><p>{t('选择适合你的发布节奏，额度与价格一目了然。')}</p></div></div><div className="home-pricing-grid">{config.plans.map(plan => { const paid = plan.id !== 'free'; return <article className={`home-plan ${paid ? 'home-plan-paid' : ''}`} key={plan.id}><div className="home-plan-top"><span>{paid ? t('为更进一步而准备') : t('让第一个想法出发')}</span><ArrowUpRight size={20} /></div><h3>{localizedPlanName(plan.name)}</h3><div className="home-plan-price"><span>$</span><strong>{money(plan.price_micros, plan.price_micros % 1e6 === 0 ? 0 : 2)}</strong><small>{paid ? t('/ 月') : t('/ 免费')}</small></div><Link className={`home-button ${paid ? '' : 'home-button-outline'}`} to={startPath}>{config.registration_open ? paid ? t('选择{0}',{0:localizedPlanName(plan.name)}) : t('免费创建账户') : t('登录查看套餐')}<ArrowUpRight size={17} /></Link><ul><li><Check size={15} /><span><strong>{number(plan.app_limit)}</strong> {t(' 个应用名额')}</span></li><li><Check size={15} /><span><strong>{bytes(plan.storage_bytes)}</strong> {t(' 存储空间')}</span></li><li><Check size={15} /><span>{paid ? t('每个付费月赠送') : t('注册赠送')} <strong>{number(plan.download_limit)}</strong> {t(' 次下载')}</span></li><li><Check size={15} /><span>{t('自定义下载域名与下载统计')}</span></li>{paid && <li><ShieldCheck size={15} /><span>{t('自动检查域名安全')}</span></li>}</ul><p className="home-plan-footnote">{paid ? t('每个付费月发放下载额度，续费后再次赠送。安全检测需在应用分流中开启。') : t('注册赠送一次，剩余下载次数保留至用完。')}</p></article>; })}</div><p className="home-pricing-note">{t('赠送次数用完后，按 $')}{money(config.download_price_micros,3)} {t(' / 次扣除账户余额。')}</p></section>

</>}
      <section className="home-width home-faq" id="home-faq" aria-labelledby="home-faq-heading"><div className="home-section-top"><span className="home-eyebrow">06 / NOTES & ANSWERS</span><div><h2 id="home-faq-heading">{content.faq.heading}</h2><p>{content.faq.intro}</p></div></div><div className="home-faq-list">{content.faq.items.map((item,index) => <details key={item.question} open={index===0}><summary>{item.question}<span aria-hidden="true">+</span></summary><p>{item.answer}</p></details>)}</div></section>
      <section className="home-width home-final-cta"><span className="home-eyebrow">THE NEXT CHAPTER IS YOURS.</span><div><h2>{t('下一次发布，就从这里开始。')}</h2><Link className="home-button" to={startPath}>{config.registration_open ? t('创建你的账户') : t('进入控制台')}<ArrowUpRight size={20} /></Link></div></section>
    </main><footer className="home-width home-footer"><Link to={`/welcome${inviteQuery}`}><Brand /></Link><span>© {new Date().getFullYear()} {config.site_name} / APK · PWA</span><Link to={`/login${inviteQuery}`}>{t('进入控制台')}<ArrowUpRight size={13} /></Link></footer>
  </div>;
}
