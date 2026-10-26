// i18n: static UI literals
import { useEffect, useId, useRef, useState, type Ref } from 'react';
import { Link } from 'react-router-dom';
import { Check, ChevronDown, Code2, Copy, LayoutTemplate, LoaderCircle } from 'lucide-react';
import { t, useLocale } from './i18n';
import { getDIYLandingAIPrompt, getDIYLandingHelpQuestions, getDIYLandingHelpSteps, getDIYPrimaryButtonExample } from './diy-landing-help-content';
import './help-diy-landing.css';

export function DIYLandingHelp({ sectionRef }: { sectionRef?: Ref<HTMLElement> }) {
  const locale = useLocale();
  const headingID = useId(), promptID = useId();
  const prompt = getDIYLandingAIPrompt(), example = getDIYPrimaryButtonExample();
  const steps = getDIYLandingHelpSteps(), questions = getDIYLandingHelpQuestions();
  const textarea = useRef<HTMLTextAreaElement>(null), latestPrompt = useRef(prompt), request = useRef(0), mounted = useRef(true);
  const [copyState, setCopyState] = useState<{ state: 'idle' | 'copying' | 'copied' | 'manual'; locale: string }>({ state: 'idle', locale });
  latestPrompt.current = prompt;
  const state = copyState.locale === locale ? copyState.state : 'idle';

  useEffect(() => { request.current++; setCopyState({ state: 'idle', locale }); }, [locale, prompt]);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; request.current++; };
  }, []);

  const copyPrompt = async () => {
    const current = ++request.current, value = prompt;
    setCopyState({ state: 'copying', locale });
    const active = () => mounted.current && request.current === current && latestPrompt.current === value;
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(value);
      if (active()) setCopyState({ state: 'copied', locale });
    } catch {
      if (!active()) return;
      textarea.current?.focus({ preventScroll: true });
      textarea.current?.select();
      textarea.current?.setSelectionRange(0, value.length);
      setCopyState({ state: 'manual', locale });
    }
  };

  return <section id="help-diy-landings" ref={sectionRef} tabIndex={-1} className="help-diy-landing panel padded" aria-labelledby={headingID}>
    <header className="help-diy-header"><div><h2 id={headingID}><Code2 size={22} strokeWidth={1.6}/>{t('DIY落地页常用问题')}</h2><p>{t('从模版选择到 AI 生成，按步骤制作并编辑自己的落地页。')}</p></div><nav aria-label={t('DIY落地页说明')}><Link className="btn primary" to="/diy-landings"><Code2 size={15}/>{t('打开DIY落地页')}</Link><Link className="btn secondary" to="/landing-templates"><LayoutTemplate size={15}/>{t('浏览模版库')}</Link></nav></header>
    <div className="help-diy-instructions">
      <div className="help-diy-workflow"><h3>{t('制作与使用')}</h3><ol>{steps.map(step => <li key={step.title}><h4>{step.title}</h4><p>{step.body}</p></li>)}</ol><div className="help-diy-button-example"><h3>{t('主按钮接入示例')}</h3><pre><code>{example}</code></pre></div></div>
      <div className="help-diy-prompt"><div className="help-diy-prompt-heading"><label htmlFor={promptID}>{t('将以下要求发给 AI')}</label><button type="button" className="btn secondary" disabled={state === 'copying'} onClick={() => { void copyPrompt(); }}>{state === 'copying' ? <LoaderCircle size={15} className="spin"/> : state === 'copied' ? <Check size={15}/> : <Copy size={15}/ >}{t('复制 AI 提示词')}</button></div><textarea id={promptID} ref={textarea} aria-label={t('AI 提示词')} readOnly spellCheck={false} rows={18} wrap="soft" value={prompt}/><p className={`help-diy-copy-status ${state === 'manual' ? 'manual' : ''}`} role="status" aria-live="polite" aria-atomic="true">{state === 'copied' ? t('已复制 AI 提示词，可粘贴给 AI。') : state === 'manual' ? t('复制未完成，已选中全部提示词，请手动复制。') : ''}</p><p className="help-diy-prompt-note">{t('提示词只要求生成页面内容，下载、安装和计费沿用应用设置。')}</p></div>
    </div>
    <div className="help-diy-questions">{questions.map(item => <details key={item.question}><summary><span>{item.question}</span><ChevronDown size={17}/></summary><div>{item.paragraphs.map((paragraph, index) => <p key={index}>{paragraph}</p>)}</div></details>)}</div>
  </section>;
}
