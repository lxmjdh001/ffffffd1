import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Root } from '../src/root';

const script = readFileSync(resolve('public/assets/home-loading-v4.js'), 'utf8');
beforeEach(() => {
 vi.useFakeTimers();
 window.history.replaceState(null, '', '/#/welcome');
 document.body.innerHTML = '<div id="root"><main>Homepage preview</main></div><div id="home-loading" hidden><span></span></div>';
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({site_name:'D1 Apk'}))));
});
afterEach(() => {
 vi.runOnlyPendingTimers(); vi.useRealTimers(); cleanup();
 delete window.__d1HomeConfig;
 document.documentElement.removeAttribute('data-home-loading');
 document.body.innerHTML='';localStorage.clear();vi.unstubAllGlobals();
 window.history.replaceState(null,'','/');
});
function start() {
 runInNewContext(script, { window, document, location: window.location, localStorage, fetch, Date, setTimeout, clearTimeout });
}
it('starts a blurred overlay and fetches configuration before the main app',async()=>{
 start();
 expect(document.documentElement.hasAttribute('data-home-loading')).toBe(true);
 expect(document.getElementById('home-loading')!.hidden).toBe(false);
 expect(document.getElementById('root')!.inert).toBe(true);
 expect(fetch).toHaveBeenCalledWith('/api/config',{credentials:'same-origin'});
 expect(await window.__d1HomeConfig).toMatchObject({site_name:'D1 Apk'});
 window.dispatchEvent(new Event('d1:home-ready'));
 vi.advanceTimersByTime(999);expect(document.getElementById('home-loading')!.hidden).toBe(false);
 vi.advanceTimersByTime(1);expect(document.getElementById('home-loading')!.hidden).toBe(true);
 expect(document.documentElement.hasAttribute('data-home-loading')).toBe(false);
});
it('offers recovery on a slow connection without exposing an unfinished homepage',()=>{
 start();vi.advanceTimersByTime(12000);
 expect(document.getElementById('home-loading')!.hidden).toBe(false);
 vi.advanceTimersByTime(3000);
 expect(screen.getByRole('button',{name:'Retry'})).toBeTruthy();
 expect(document.getElementById('root')!.inert).toBe(true);
 window.dispatchEvent(new Event('d1:home-ready'));vi.advanceTimersByTime(0);
 expect(document.getElementById('home-loading')!.hidden).toBe(true);
});
it('keeps the animation visible during a slow first load instead of hiding at two seconds',()=>{
 start();vi.advanceTimersByTime(3000);
 expect(document.getElementById('home-loading')!.hidden).toBe(false);
 expect(document.getElementById('root')!.inert).toBe(true);
 window.dispatchEvent(new Event('d1:home-ready'));vi.advanceTimersByTime(0);
 expect(document.getElementById('home-loading')!.hidden).toBe(true);
});
it('uses an independent parser-time bootstrap rather than waiting for deferred app modules',()=>{
 const html=readFileSync(resolve('index.html'),'utf8');
 const tag=html.match(/<script\b[^>]*src="\/assets\/home-loading-v4\.js"[^>]*>/)?.[0];
 expect(tag).toBeTruthy();
 expect(tag).not.toMatch(/\bdefer\b|type="module"/);
});
it('always uses English even when the selected language is Chinese',()=>{
 localStorage.setItem('d1.locale','zh-CN');
 document.documentElement.lang='zh-CN';
 start();
 expect(document.getElementById('home-loading')!.textContent).toBe('Loading...');
});
it('does not show the homepage loader or prefetch config on a login route',()=>{
 window.history.replaceState(null,'','/#/login');start();
 expect(document.getElementById('home-loading')!.hidden).toBe(true);
 expect(fetch).not.toHaveBeenCalled();
});
it('releases the overlay immediately when navigating away',()=>{
 start();window.history.replaceState(null,'','/#/register');
 window.dispatchEvent(new Event('hashchange'));
 expect(document.getElementById('home-loading')!.hidden).toBe(true);
});
it('lets React retry if configuration prefetch fails',async()=>{
 vi.mocked(fetch).mockRejectedValue(new Error('offline'));start();
 expect(await window.__d1HomeConfig).toBeUndefined();
});
it('preserves the static homepage while configuration is still pending',()=>{
 window.__d1HomeConfig=new Promise(()=>{});
 render(<Root initialHomeHTML="<main>Existing homepage content</main>"/>);
 expect(screen.getByText('Existing homepage content')).toBeTruthy();
 expect(screen.queryByText('正在加载…')).toBeNull();
 expect(fetch).not.toHaveBeenCalled();
});

it('offers an immediate retry when config loading fails',()=>{start();window.dispatchEvent(new Event('d1:home-error'));expect(screen.getByRole('button',{name:'Retry'})).toBeTruthy();window.dispatchEvent(new Event('d1:home-ready'));vi.advanceTimersByTime(1000);});
