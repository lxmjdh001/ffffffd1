// i18n: static UI literals
import { localizedPlanName, t, useLocale } from './i18n';
import { Link, useLocation } from 'react-router-dom';
import { ArrowDownRight, ArrowRight, ArrowUpRight, Check, CheckCheck, CloudUpload, Copy, Globe2, Layers3, Link2, ListFilter, Monitor, Package, Radio, ShieldCheck, Smartphone, Sparkles } from 'lucide-react';
import { Brand } from './auth';
import { bytes, money, number, useConfig } from './ui';
import originalHomeContent from './home-content.json';
import { translateContent } from './i18n';
import { HomeNavigation } from './home-navigation';
import './landing.css';

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
}

function ReleasePreview() {
  useLocale();
  const { config } = useConfig();
  let host = 'd1apk.com';
  try { host = new URL(config.public_url).host; } catch { /* Use the brand domain in the illustrative preview. */ }
  return <div className="home-preview" role="img" aria-label={t("应用上传、生成链接与分享的叠层界面示意")}>
    <div className="home-preview-grid" />
    <div className="home-preview-orbit" />
    <div className="home-sheet home-sheet-back"><span>FROM FILE TO LINK</span><Layers3 size={20} /></div>
    <div className="home-sheet home-sheet-middle" />
    <div className="home-release-window">
      <div className="home-window-bar"><div><i /><i /><i /></div><span>{t("应用分发工作台")}</span><span>{t("界面示意")}</span></div>
      <div className="home-window-body">
        <div className="home-preview-file"><div className="home-package-icon"><Package size={33} strokeWidth={1.45} /></div><div><strong>{t("你的下一款应用")}</strong><span>Android · v1.0.0</span></div><span className="home-file-type">APK</span></div>
        <div className="home-upload-state"><span><CheckCheck size={15} />{t("安装包已就绪")}</span><span>100%</span></div>
        <div className="home-upload-progress"><span /></div>
        <div className="home-preview-label">{t("专属下载链接")}</div>
        <div className="home-preview-link"><Link2 size={16} /><span>{host}<b>/a/your-app</b></span><Copy size={15} /></div>
        <div className="home-window-bottom"><span><Globe2 size={14} />{t("一个链接，多端访问")}</span><span>{t("随时分享 ")}<ArrowUpRight size={14} /></span></div>
      </div>
    </div>
    <div className="home-floating-file"><span><CloudUpload size={21} strokeWidth={1.6} /></span><div><strong>{t("上传一次")}</strong><small>{t("让发布更轻松")}</small></div><Check size={15} /></div>
    <div className="home-share-card"><div className="home-share-icon"><Link2 size={22} strokeWidth={1.7} /></div><div><span>READY TO SHARE</span><strong>{t("好应用，值得被看见。")}</strong><small>{t("从这里，连接每一位用户。")}</small></div><ArrowUpRight size={19} /></div>
    <div className="home-preview-caption"><span /> {t(" 一次上传，开启下一次抵达")}</div>
  </div>;
}

export function LandingPage() {
  useLocale();
  const homeContent = translateContent(originalHomeContent);
  const { config } = useConfig();
  const location = useLocation();
  const invite = new URLSearchParams(location.search).get('invite') ?? new URLSearchParams(window.location.search).get('invite');
  const inviteQuery = invite === null ? '' : `?invite=${encodeURIComponent(invite.trim())}`;
  const startPath = `${config.registration_open ? '/register' : '/login'}${inviteQuery}`;
  return <div className="home">
    <HomeNavigation inviteQuery={inviteQuery} startPath={startPath} />
    <main>
      <section className="home-hero">
        <div className="home-hero-wash" />
        <div className="home-width home-hero-layout">
          <div className="home-hero-copy">
            <div className="home-kicker"><span /><span>{homeContent.hero.kicker}</span></div>
            <h1>{homeContent.hero.lines[0]}<br /><span>{homeContent.hero.lines[1]}</span></h1>
            <p>{homeContent.hero.description}</p>
            <div className="home-hero-actions"><Link className="home-button" to={startPath}>{config.registration_open ? t("开始免费分发") : t("登录控制台")}<ArrowRight size={18} /></Link><button className="home-text-button" onClick={() => scrollToSection('home-how')}>{t("看看如何开始")}<ArrowDownRight size={17} /></button></div>
            <div className="home-hero-note"><Check size={14} /><span>{config.registration_open ? t("免费开启你的第一个应用 · 随时按需升级") : t("注册暂未开放 · 已有账户可登录")}</span></div>
          </div>
          <ReleasePreview />
        </div>
        <div className="home-width home-platform-strip"><p>{t("一个入口，")}<strong>{t("连接每个平台。")}</strong></p><div><span><Smartphone size={20} strokeWidth={1.5} />Android</span><span><Smartphone size={20} strokeWidth={1.5} />iOS</span><span><Monitor size={21} strokeWidth={1.5} />Windows</span><span><Monitor size={21} strokeWidth={1.5} />macOS</span></div></div>
      </section>

      <section className="home-width home-solutions" id="home-solutions" aria-labelledby="home-solutions-heading">
        <div className="home-section-top"><div><div className="home-eyebrow">ONE WORKSPACE, EVERY DOWNLOAD</div><h2 id="home-solutions-heading">{homeContent.solutions.heading}</h2></div><p>{homeContent.solutions.intro}</p></div>
        <div className="home-solution-grid">{homeContent.solutions.items.map((item, index) => {
          const Icon = [Package, Link2, ListFilter, Monitor, Radio][index];
          return <article key={item.id} className={item.id === 'ads' ? 'home-solution-ads' : undefined}>
            <div className="home-solution-top"><span className="home-solution-icon"><Icon size={23} strokeWidth={1.5} /></span><span>{item.label}</span></div>
            <h3>{item.title}</h3><p>{item.description}</p>
            <ul aria-label={t("{0}功能", { 0: item.title })}>{item.tags.map(tag => <li key={tag}>{tag}</li>)}</ul>
            {item.id === 'ads' && <div className="home-ad-flow" aria-label={t("广告点击、完整下载和接入事件连接到 Meta 或 TikTok")}><span>{t("广告点击")}</span><ArrowRight size={16} /><span>{t("完整下载")}</span><ArrowRight size={16} /><span>{t("打开 / 注册")}</span><ArrowRight size={16} /><strong>Meta · TikTok</strong></div>}
          </article>;
        })}</div>
      </section>

      <section className="home-width home-how" id="home-how" aria-labelledby="home-how-heading">
        <div className="home-section-top"><div><div className="home-eyebrow">A LIGHTER WAY TO LAUNCH</div><h2 id="home-how-heading">{t("从上传到分享，")}<br className="home-mobile-break" />{t("只需三步。")}</h2></div><p>{t("把精力留给创造，")}<br />{t("把交付交给 ")}{config.site_name}。</p></div>
        <div className="home-steps">
          <article><div className="home-step-top"><span>01</span><CloudUpload size={26} strokeWidth={1.35} /></div><h3>{t("上传你的应用")}</h3><p>{t("支持 APK、IPA 与桌面安装包，")}<br />{t("在同一空间管理应用与版本。")}</p><span className="home-step-line" /></article>
          <article><div className="home-step-top"><span>02</span><Link2 size={26} strokeWidth={1.35} /></div><h3>{t("获得专属链接")}</h3><p>{t("为应用配置下载入口，")}<br />{t("也可以使用自己的品牌域名。")}</p><span className="home-step-line" /></article>
          <article><div className="home-step-top"><span>03</span><ArrowUpRight size={26} strokeWidth={1.35} /></div><h3>{t("分享，即刻出发")}</h3><p>{t("把链接分享给你的用户，")}<br />{t("在控制台查看每一次下载。")}</p></article>
        </div>
      </section>

      <section className="home-features-wrap" id="home-features" aria-labelledby="home-features-heading"><div className="home-width">
        <div className="home-section-top"><div><div className="home-eyebrow">THOUGHTFUL IN EVERY DETAIL</div><h2 id="home-features-heading">{t("轻盈的体验，")}<br className="home-mobile-break" />{t("周全的交付。")}</h2></div><span className="home-section-note">{t("应用分发需要的，都在这里。")}</span></div>
        <div className="home-feature-grid">
          <article className="home-feature-domain"><div className="home-feature-icon"><Globe2 size={23} strokeWidth={1.5} /></div><h3>{t("你的域名，你的品牌")}</h3><p>{t("使用自己的下载域名，让每一次访问，都延续你的品牌体验。")}</p><div className="home-domain-illustration" aria-hidden="true"><Globe2 size={17} /><span>download.yourbrand.com</span><Link2 size={14} /></div></article>
          <article className="home-feature-insights"><div className="home-feature-icon"><Layers3 size={23} strokeWidth={1.5} /></div><h3>{t("管理更有条理")}</h3><p>{t("应用版本、下载记录和账户用量，在一个清晰的工作空间中掌握。")}</p><div className="home-insight-illustration" aria-hidden="true"><span><Package size={15} />{t("应用管理")}<Check size={13} /></span><span><ArrowDownRight size={15} />{t("下载记录")}<Check size={13} /></span></div></article>
          <article className="home-feature-cloak"><div className="home-feature-icon"><ListFilter size={23} strokeWidth={1.5} /></div><h3>{t("UA 拉黑，按账户独立生效")}</h3><p>{t("在下载记录中查看并拉黑 UA，被拉黑的 UA 再次访问时显示专用英文 404。黑名单按账户隔离，也可以随时解除拉黑。")}</p><div className="home-cloak-illustration" aria-hidden="true"><span><ListFilter size={16} />{t("UA 访问规则")}</span><ArrowRight size={15} /><div><span><Link2 size={14} />{t("正常下载")}</span><span><ShieldCheck size={14} />{t("专用 404")}</span></div></div></article>
          <article className="home-feature-security"><div className="home-feature-icon"><ShieldCheck size={23} strokeWidth={1.5} /></div><h3>{t("自动检测域名安全性")}</h3><p>{t("付费套餐在应用分流中开启安全检测后，系统定时检查域名风险。命中风险的域名会自动停止参与分流，让你及时掌握下载入口状态。")}</p><div className="home-security-illustration" aria-hidden="true"><ShieldCheck size={34} strokeWidth={1.3} /><div><strong>{t("定时检测 · 风险自动排除")}</strong><span>{t("付费套餐专属权益")}</span></div></div></article>
        </div>
      </div></section>

      <section className="home-width home-pricing" id="home-pricing" aria-labelledby="home-pricing-heading">
        <div className="home-pricing-heading"><div className="home-eyebrow">ROOM FOR YOUR NEXT BIG IDEA</div><h2 id="home-pricing-heading">{t("从免费开始，")}<span>{t("随成长升级。")}</span></h2><p>{t("选择适合你的发布节奏，额度与价格一目了然。")}</p></div>
        <div className="home-pricing-grid">{config.plans.map(plan => {
          const paid = plan.id !== 'free';
          return <article className={`home-plan ${paid ? 'home-plan-paid' : ''}`} key={plan.id}>
            <div className="home-plan-top"><span>{paid ? t("为更进一步而准备") : t("让第一个想法出发")}</span>{paid && <span className="home-plan-recommendation"><Sparkles size={12} />{t("推荐选择")}</span>}</div>
            <h3>{localizedPlanName(plan.name)}</h3><p className="home-plan-description">{paid ? t("更多应用、更大空间，为持续发布而准备。") : t("小步开启，让你的应用拥有第一个下载入口。")}</p>
            <div className="home-plan-price"><span>$</span><strong>{money(plan.price_micros, plan.price_micros % 1e6 === 0 ? 0 : 2)}</strong><small>{paid ? t("/ 月") : t("/ 免费")}</small></div>
            <Link className={`home-button ${paid ? '' : 'home-button-outline'}`} to={startPath}>{config.registration_open ? paid ? t("选择{0}", { 0: localizedPlanName(plan.name) }) : t("免费创建账户") : t("登录查看套餐")}<ArrowUpRight size={17} /></Link>
            <div className="home-plan-divider" /><span className="home-plan-includes">{paid ? t("为你的日常发布提供更多支持") : t("包含开启分发所需的基础功能")}</span>
            <ul><li><Check size={16} /><span><strong>{number(plan.app_limit)}</strong> {t(" 个应用名额")}</span></li><li><Check size={16} /><span><strong>{bytes(plan.storage_bytes)}</strong> {t(" 存储空间")}</span></li><li><Check size={16} /><span>{paid ? t("每个付费月赠送") : t("注册赠送")} <strong>{number(plan.download_limit)}</strong> {t(" 次下载")}</span></li><li><Check size={16} /><span>{t("自定义下载域名与下载统计")}</span></li>{paid && <li className="home-plan-security"><ShieldCheck size={16} /><span>{t("自动检查域名安全")}</span></li>}</ul>
            <p className="home-plan-footnote">{paid ? t("每个付费月发放下载额度，续费后再次赠送。安全检测需在应用分流中开启。") : t("注册赠送一次，剩余下载次数保留至用完。")}</p>
          </article>;
        })}</div>
        <p className="home-pricing-note">{t("赠送次数用完后，按 $")}{money(config.download_price_micros, 3)} {t(" / 次扣除账户余额。")}</p>
      </section>

      <section className="home-width home-faq" id="home-faq" aria-labelledby="home-faq-heading">
        <div className="home-section-top"><div><div className="home-eyebrow">BEFORE YOUR FIRST RELEASE</div><h2 id="home-faq-heading">{homeContent.faq.heading}</h2></div><p>{homeContent.faq.intro}</p></div>
        <div className="home-faq-list">{homeContent.faq.items.map((item, index) => <details key={item.question} open={index === 0}><summary>{item.question}<span aria-hidden="true">+</span></summary><p>{item.answer}</p></details>)}</div>
      </section>

      <section className="home-width home-final-cta"><div className="home-cta-layers" aria-hidden="true"><span /><span /><span><CloudUpload size={28} strokeWidth={1.3} /></span></div><div><span>YOUR NEXT RELEASE STARTS HERE</span><h2>{t("下一次发布，就从这里开始。")}</h2><p>{t("让应用出发，让好想法被更多人看见。")}</p></div><Link className="home-button" to={startPath}>{config.registration_open ? t("创建你的账户") : t("进入控制台")}<ArrowRight size={17} /></Link></section>
    </main>
    <footer className="home-width home-footer"><Link to={`/welcome${inviteQuery}`}><Brand /></Link><span>© {new Date().getFullYear()} {config.site_name} {t(" · 让每一次交付，都简单一点。")}</span><Link to={`/login${inviteQuery}`}>{t("进入控制台")}<ArrowUpRight size={13} /></Link></footer>
  </div>;
}
