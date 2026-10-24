import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {runInNewContext} from 'node:vm';
import {JSDOM} from 'jsdom';
import {expect,it} from 'vitest';
const code=readFileSync(resolve(process.cwd(),'../internal/platform/templates/apk-landing-bridge.js'),'utf8');
const visibleText=(element:Element)=>{const clone=element.cloneNode(true) as Element;clone.querySelectorAll('.d1-text-noise').forEach(n=>n.remove());return clone.textContent;};
function obfuscationFixture(preview=false,enabled=true){
 const dom=new JSDOM(`<body class="d1-apkabc123-reference ${preview?'d1-apk-preview':''}" data-d1-apk-reference><div id="app"><h1 class="app-title">LINK APP</h1><a class="app-developer-link" href="/brand">LINK Studio</a><button class="install-button">Download</button></div><form id="d1-native-download" action="/a/test/download"><button>Download</button></form></body>`,{url:'https://chosen.example/c/test'});
 const win=dom.window;Object.assign(win,{__APP_CONFIG__:{d1APK:{textObfuscation:enabled,buttonText:'Download'}}});
 runInNewContext(code,{window:win,document:win.document,Node:win.Node,MutationObserver:win.MutationObserver,location:win.location});
 return {dom,win,title:win.document.querySelector('h1')!,brand:win.document.querySelector('a')!};
}
it('changes underlying name text without changing visible content, links or native downloads',async()=>{
 const {dom,win,title,brand}=obfuscationFixture();
 expect(title.textContent).not.toBe('LINK APP');expect(visibleText(title)).toBe('LINK APP');
 expect(title.getAttribute('aria-label')).toBe('LINK APP');expect(brand.getAttribute('aria-label')).toBe('LINK Studio');
 expect(title.children).toHaveLength(1);expect(title.firstElementChild?.className).toBe('d1-obfuscated-text');
 expect(visibleText(brand)).toBe('LINK Studio');expect(brand.getAttribute('href')).toBe('/brand');
 expect([...title.querySelectorAll('.d1-text-noise')].every(n=>n.getAttribute('aria-hidden')==='true'&&/^[a-z0-9]{2}$/.test(n.textContent||''))).toBe(true);
 const raw=title.textContent,noiseCount=title.querySelectorAll('.d1-text-noise').length;
 win.document.querySelector<HTMLButtonElement>('form button')!.disabled=true;
 await new Promise<void>(r=>win.setTimeout(r,0));
 expect(title.textContent).toBe(raw);expect(title.querySelectorAll('.d1-text-noise')).toHaveLength(noiseCount);
 expect(win.document.querySelector<HTMLFormElement>('form')!.action).toBe('https://chosen.example/a/test/download');
 expect(win.document.querySelector('.install-button')?.textContent).toBe('Download');dom.window.close();
});
it('defaults off and restores names immediately when the preview switch is disabled',async()=>{
 const off=obfuscationFixture(false,false);expect(off.title.textContent).toBe('LINK APP');expect(off.title.querySelector('.d1-text-noise')).toBeNull();off.dom.window.close();
 const {dom,win,title,brand}=obfuscationFixture(true);
 win.dispatchEvent(new win.MessageEvent('message',{origin:win.location.origin,source:win.parent,data:{type:'UPDATE_APP_INFO',data:{d1APK:{textObfuscation:false,buttonText:'Download'}}}}));
 expect(title.textContent).toBe('LINK APP');expect(brand.textContent).toBe('LINK Studio');expect(win.document.querySelector('.d1-text-noise')).toBeNull();dom.window.close();
});
it('handles Vue text-node and full-element language changes without stale characters',async()=>{
 const {dom,win,title}=obfuscationFixture();
 // Vue may retain its original text node and write a one-character name.
 title.querySelector('.d1-obfuscated-value')!.firstChild!.nodeValue='L';await new Promise<void>(r=>win.setTimeout(r,0));expect(visibleText(title)).toBe('L');
 title.textContent='繁體 👩‍💻 e\u0301';await new Promise<void>(r=>win.setTimeout(r,0));
 expect(visibleText(title)).toBe('繁體 👩‍💻 e\u0301');
 expect([...title.querySelector('.d1-obfuscated-value')!.childNodes].filter(n=>n.nodeType===3).map(n=>n.textContent)).toContain('👩‍💻');
 expect([...title.querySelector('.d1-obfuscated-value')!.childNodes].filter(n=>n.nodeType===3).map(n=>n.textContent)).toContain('e\u0301');dom.window.close();
});
it('keeps reference buttons and ratings synchronized with the original native download form',async()=>{
 const dom=new JSDOM('<body class="d1-apkdef456-reference" data-d1-apk-reference><div id="app"><button class="install-button">Install</button><div class="app-stats-row"><div class="stat-col"><div class="stat-main">4.9<span>★</span></div></div></div><div class="large-rating">4.9</div><section class="ratings-section"></section></div><form id="d1-native-download"><button>Native</button></form><div id="d1-download-extras">Status</div><div id="d1-file-info">File metadata</div></body>',{url:'https://d1.example/a/app/info'});
 const win=dom.window;Object.assign(win,{__APP_CONFIG__:{d1APK:{buttonText:'Download APK',rating:'4.8',showDownloads:true,showDescription:true,showScreenshots:true}}});
 runInNewContext(code,{window:win,document:win.document,Node:win.Node,MutationObserver:win.MutationObserver,location:win.location});
 const native=win.document.querySelector<HTMLButtonElement>('form button')!;const visible=win.document.querySelector<HTMLButtonElement>('.install-button')!;
 expect(visible.textContent).toBe('Download APK');expect(win.document.querySelector('.large-rating')?.textContent).toBe('4.8');expect(win.document.querySelector('form')?.hidden).toBe(true);expect(visible.nextElementSibling?.id).toBe('d1-download-extras');
 native.disabled=true;await new Promise<void>(r=>win.setTimeout(r,0));expect(visible.disabled).toBe(true);native.disabled=false;await new Promise<void>(r=>win.setTimeout(r,0));expect(visible.disabled).toBe(false);
 dom.window.close();
});
