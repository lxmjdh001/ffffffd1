// i18n: static UI literals
import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { ArrowLeft, ArrowUpRight, BookOpen, CloudUpload, Globe2 } from 'lucide-react';
import { t, useLocale } from './i18n';
import { DIYLandingHelp } from './help-diy-landing';
import { PageHeader, useConfig } from './ui';

export function HelpPage() {
  useLocale();
  const { config } = useConfig();
  const guides = [
    { icon: CloudUpload, title: t('01 · 上传与更新'), body: t('在应用管理中上传 APK、IPA、EXE、MSI、DMG、ZIP 或 AAB。上传后获取 /a/随机码 的公开分享路径。更新安装包时，原分享路径保持不变；IPA 仅提供安装包下载，不提供苹果签名或绕过设备授权。'), link: '/apps', label: t('前往应用管理') },
    { icon: Globe2, title: t('02 · 绑定自己域名'), body: t('在域名管理添加你拥有的子域名。创建 CNAME 指向 {0}，验证 CNAME 生效后即可使用。使用仅 DNS（灰云），验证通过后首次访问自动启用 HTTPS。官网域名只用于登录和管理。', { 0: config.cname_target }), link: '/domains', label: t('管理下载域名') },
  ];
  return <>
    <PageHeader eyebrow="GETTING STARTED" title={t('使用指南')} subtitle={t('从第一次上传，到每一次交付。')}/>
    <div className="help-grid">
      {guides.map(guide => <section className="panel padded" key={guide.link}>
        <div className="help-icon"><guide.icon size={22}/></div>
        <h2>{guide.title}</h2><p>{guide.body}</p>
        <Link className="text-btn" to={guide.link}>{guide.label} →</Link>
      </section>)}
      <Link to="/help/diy-landings" className="panel padded help-guide-card" aria-labelledby="diy-guide-card-title">
        <div className="help-guide-card-top"><div className="help-icon"><BookOpen size={22}/></div><ArrowUpRight size={20} aria-hidden="true"/></div>
        <h2 id="diy-guide-card-title">{t('DIY落地页说明')}</h2>
        <p>{t('从模版选择到 AI 生成，按步骤制作并编辑自己的落地页。')}</p>
        <span className="text-btn">{t('DIY落地页常用问题')} →</span>
      </Link>
    </div>
  </>;
}

export function DIYLandingGuidePage() {
  useLocale();
  useEffect(() => { window.scrollTo({ top: 0, left: 0, behavior: 'instant' }); }, []);
  return <div className="help-diy-guide">
    <Link to="/help" className="back-link"><ArrowLeft size={15}/>{t('使用指南')}</Link>
    <PageHeader title={t('DIY落地页说明')} subtitle={t('从模版选择到 AI 生成，按步骤制作并编辑自己的落地页。')}/>
    <DIYLandingHelp/>
  </div>;
}
