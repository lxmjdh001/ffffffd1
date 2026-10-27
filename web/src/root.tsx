import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { HashRouter, Route, Routes } from 'react-router-dom';
import { api } from './api';
import { initializeLocale, useLocale } from './i18n';
import type { Config } from './types';
import { ConfigContext, ErrorState, Spinner } from './ui';
import { LandingPage } from './landing';
import { RouteMetadata, usePageMetadata } from './home-seo';

const ConsoleRoot = lazy(() => import('./console-root'));

declare global {
  interface Window { __d1HomeConfig?: Promise<Config | undefined> }
}

export function Root({ initialHomeHTML = '' }: { initialHomeHTML?: string }) {
  useLocale();
  const [config, setConfig] = useState<Config>();
  const [error, setError] = useState<unknown>();
  const reload = useCallback(() => {
    const prefetched = window.__d1HomeConfig;
    const request = prefetched ? prefetched.then(c => c ?? api<Config>('/config')) : api<Config>('/config');
    request.then(c => { delete window.__d1HomeConfig; initializeLocale(c.country_code); setConfig(c); setError(undefined); }).catch(failure => { delete window.__d1HomeConfig; setError(failure); window.dispatchEvent(new Event('d1:home-error')); });
  }, []);
  useEffect(reload, [reload]);
  usePageMetadata(config?.site_name, config?.public_url);
  if (error) return <ErrorState error={error} retry={reload} />;
  if (!config) {
    const home = !window.location.hash || window.location.hash === '#' || /^#\/welcome(?:\?|$)/.test(window.location.hash);
    return home && initialHomeHTML ? <div dangerouslySetInnerHTML={{ __html: initialHomeHTML }} /> : <Spinner />;
  }
  return <ConfigContext.Provider value={{ config, reload }}><HashRouter><RouteMetadata siteName={config.site_name} publicURL={config.public_url} /><Routes><Route path="/welcome" element={<LandingPage />} /><Route path="*" element={<Suspense fallback={<Spinner />}><ConsoleRoot /></Suspense>} /></Routes></HashRouter></ConfigContext.Provider>;
}
