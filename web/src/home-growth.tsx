import { useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, ArrowUpRight, Check, Cloud, Code2, Database, ExternalLink, Globe2, Layers3, Monitor, MousePointer2, RotateCcw, ShieldCheck, Smartphone, Sparkles, Type } from 'lucide-react';
import type homeContent from './home-content.json';

type GrowthContent = typeof homeContent.growth;
const icons = { cloud: Cloud, code: Code2, text: Type, diy: Layers3, browser: ExternalLink, cloak: ShieldCheck, return: RotateCcw };

export function HomeGrowthFeatures({ content, startPath }: { content: GrowthContent; startPath: string }) {
  const [theme, setTheme] = useState(0);
  const labels = content.labels;
  const visual = (id: string) => {
    switch (id) {
      case 'cloud': return <div className="growth-edge-map" aria-hidden="true"><div className="growth-edge-source"><Cloud size={35}/><strong>Cloudflare</strong><span>R2 + CDN</span></div><div className="growth-edge-nodes">{['AMERICAS', 'EUROPE', 'ASIA PACIFIC'].map(region => <div key={region}><Globe2 size={17}/><span>{region}</span><i/></div>)}</div><div className="growth-edge-device"><Smartphone size={28}/><Check size={16}/></div></div>;
      case 'code': return <div className="growth-code-visual" aria-hidden="true"><div className="growth-same-pages"><div><span className="growth-app-icon">D</span><strong>D1 Studio</strong><i/></div><span>=</span><div><span className="growth-app-icon">D</span><strong>D1 Studio</strong><i/></div></div><span className="growth-visual-label">{labels.codeBefore}</span><div className="growth-code-pair"><code><span>&lt;div class="a7f"&gt;</span><br/>&nbsp;D1 Studio<br/>&lt;/div&gt;</code><code><span>&lt;div class="k9x"&gt;</span><br/>&nbsp;D1 Studio<br/>&lt;/div&gt;</code></div><span className="growth-visual-label">{labels.codeAfter}</span></div>;
      case 'text': return <div className="growth-text-visual" aria-hidden="true"><div className="growth-text-human"><span><Monitor size={14}/>{labels.visibleText}</span><strong>D1 Studio<Check size={18}/></strong></div><div className="growth-text-source"><span><Code2 size={14}/>{labels.sourceText}</span><code><b>D</b><i>k7</i><b>1</b><i>x2</i> <b>St</b><i>m9</i><b>udio</b></code></div><div className="growth-text-baseline"><span>D1 Studio</span><i/><span>D1 Studio</span></div></div>;
      case 'diy': return <div className="growth-diy-visual"><div className="growth-theme-tabs" role="group" aria-label={content.items.find(item => item.id === 'diy')!.title}>{labels.themes.map((label, index) => <button key={index} type="button" aria-pressed={theme === index} onClick={() => setTheme(index)}>{label}</button>)}</div><div className="growth-diy-scene" data-theme={theme} aria-hidden="true"><div className="growth-editor-mini"><div><i/><i/><i/><span>index.html</span></div><code>&lt;main&gt;<br/>&nbsp; &lt;h1&gt;<em>D1 Studio</em>&lt;/h1&gt;<br/>&nbsp; &lt;button&gt;<br/>&nbsp; &nbsp; {labels.install}<br/>&nbsp; &lt;/button&gt;<br/>&lt;/main&gt;</code></div><ArrowRight className="growth-diy-arrow" size={24}/><div className="growth-diy-phone"><span className="growth-app-icon">D</span><strong>D1 Studio</strong><div className="growth-demo-lines"><i/><i/></div><span className="growth-demo-install">{labels.install}</span><small>APK + PWA</small></div></div></div>;
      case 'browser': return <div className="growth-browser-visual" aria-hidden="true"><div className="growth-social-pair"><span className="growth-tiktok">TikTok</span><span className="growth-facebook">Facebook</span></div><div className="growth-route-line"><i/><ArrowRight size={22}/><i/></div><div className="growth-browser-window"><div><i/><i/><i/><span>d1apk.com</span><ExternalLink size={13}/></div><span className="growth-app-icon">D</span><strong>D1 Studio</strong><span className="growth-demo-install">{labels.install}</span></div></div>;
      case 'cloak': return <div className="growth-cloak-visual" aria-hidden="true"><div className="growth-data-orbit"><i/><i/><i/><span><Database size={27}/></span></div><div className="growth-data-stats"><div><strong>7</strong><span>{labels.years}</span></div><div><strong>{labels.million}</strong><span>{labels.data}</span></div></div></div>;
      case 'return': return <div className="growth-return-visual" aria-hidden="true"><div className="growth-return-step"><RotateCcw size={24}/><span>{labels.back}</span></div><ArrowRight size={20}/><div className="growth-return-poster"><span>D1 Studio</span><div/><strong>{labels.again}</strong><span className="growth-demo-install">{labels.continue}<MousePointer2 size={15}/></span></div></div>;
      default: return null;
    }
  };

  return <section className="home-growth-features" id="home-growth" aria-labelledby="home-growth-heading">
    <div className="home-width"><div className="growth-section-heading"><div><h2 id="home-growth-heading">{content.heading}</h2><p>{content.intro}</p></div><Sparkles size={32} strokeWidth={1.2} aria-hidden="true"/></div>
      <div className="growth-feature-grid">{content.items.map(item => {
        const Icon = icons[item.id as keyof typeof icons];
        const metric = 'metric' in item && item.metric;
        return <article key={item.id} className={`growth-feature growth-feature-${item.id}`}>
          <div className="growth-feature-copy"><span className="growth-feature-icon"><Icon size={22} strokeWidth={1.7}/></span><h3>{item.title}</h3><p className="growth-feature-subtitle">{item.subtitle}</p><p>{item.description}</p>{metric && <div className="growth-metric"><strong>{metric}</strong><span>{item.metricLabel}</span></div>}{item.id === 'diy' && <Link to={startPath} className="growth-feature-link">{labels.create}<ArrowUpRight size={16}/></Link>}</div>
          {visual(item.id)}
        </article>;
      })}</div>
      <p className="growth-metric-note">{content.note}</p>
    </div>
  </section>;
}
