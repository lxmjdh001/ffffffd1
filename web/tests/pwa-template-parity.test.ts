import {readFileSync,existsSync} from 'node:fs';
import {resolve} from 'node:path';
import {createHash} from 'node:crypto';
import {describe,it,expect} from 'vitest';
const root=resolve(process.cwd(),'..');
const sha=(s:string)=>createHash('sha256').update(s).digest('hex');
describe('PWA source template parity',()=>{
 it('preserves the original template markup and every style rule in both templates',()=>{
  const reference=JSON.parse(readFileSync(resolve(root,'pwa-templates/reference.json'),'utf8'));
  for(const name of ['google-play','custom-app']){
   const source=readFileSync(resolve(root,`pwa-templates/${name}/src/App.vue`),'utf8').replaceAll('/pwa-template-assets/','/assets/');
   expect(sha(source.split('</template>')[0]+'</template>')).toBe(reference.components[name].template_sha256);
   expect(sha([...source.matchAll(/<style[^>]*>[\s\S]*?<\/style>/g)].map(v=>v[0]).join('\n'))).toBe(reference.components[name].styles_sha256);
   expect(source).toContain("event.origin !== window.location.origin");
  }
 });
 it('ships both built template entry points and their assets for Go embedding',()=>{
  const dir=resolve(root,'internal/platform/templates/pwa-landing');const manifest=JSON.parse(readFileSync(resolve(dir,'.vite/manifest.json'),'utf8'));
  for(const key of ['google-play/src/main.ts','custom-app/src/main.ts']){expect(existsSync(resolve(dir,manifest[key].file))).toBe(true);expect(readFileSync(resolve(dir,manifest[key].file),'utf8')).not.toContain('new Function(');for(const path of manifest[key].css)expect(existsSync(resolve(dir,path))).toBe(true);}
  for(const entry of Object.values(manifest) as {file:string}[])expect(readFileSync(resolve(dir,entry.file),'utf8')).not.toContain('new Function(');
  expect(existsSync(resolve(dir,'browser-guide.png'))).toBe(true);
 });
});
