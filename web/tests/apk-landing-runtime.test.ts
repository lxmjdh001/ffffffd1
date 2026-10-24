import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {runInNewContext} from 'node:vm';
import {JSDOM} from 'jsdom';
import {expect,it} from 'vitest';
const code=readFileSync(resolve(process.cwd(),'../internal/platform/templates/apk-landing-bridge.js'),'utf8');
it('keeps reference buttons and ratings synchronized with the original native download form',async()=>{
 const dom=new JSDOM('<body class="d1-apk-reference"><div id="app"><button class="install-button">Install</button><div class="app-stats-row"><div class="stat-col"><div class="stat-main">4.9<span>★</span></div></div></div><div class="large-rating">4.9</div><section class="ratings-section"></section></div><form id="d1-native-download"><button>Native</button></form><div id="d1-download-extras">Status</div><div id="d1-file-info">File metadata</div></body>',{url:'https://d1.example/a/app/info'});
 const win=dom.window;Object.assign(win,{__APP_CONFIG__:{d1APK:{buttonText:'Download APK',rating:'4.8',showDownloads:true,showDescription:true,showScreenshots:true}}});
 runInNewContext(code,{window:win,document:win.document,Node:win.Node,MutationObserver:win.MutationObserver,location:win.location});
 const native=win.document.querySelector<HTMLButtonElement>('form button')!;const visible=win.document.querySelector<HTMLButtonElement>('.install-button')!;
 expect(visible.textContent).toBe('Download APK');expect(win.document.querySelector('.large-rating')?.textContent).toBe('4.8');expect(win.document.querySelector('form')?.hidden).toBe(true);expect(visible.nextElementSibling?.id).toBe('d1-download-extras');
 native.disabled=true;await new Promise<void>(r=>win.setTimeout(r,0));expect(visible.disabled).toBe(true);native.disabled=false;await new Promise<void>(r=>win.setTimeout(r,0));expect(visible.disabled).toBe(false);
 dom.window.close();
});
