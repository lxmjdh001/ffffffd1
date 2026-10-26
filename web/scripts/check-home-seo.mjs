import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { JSDOM } from 'jsdom';

const content = JSON.parse(await readFile(new URL('../src/home-content.json', import.meta.url), 'utf8'));
const html = await readFile(new URL('../dist/index.html', import.meta.url), 'utf8');
// No runScripts option: this inspects the actual build without running the app.
const { document } = new JSDOM(html).window;
const canonical = document.querySelector('link[rel="canonical"]')?.getAttribute('href');
const canonicalURL = new URL(canonical || '');
assert.equal(canonical, `${canonicalURL.origin}/`, 'canonical must be the public homepage with no invitation code or hash route');
assert.equal(document.title, `D1 Apk · ${content.titleSuffix}`);
assert.equal(document.querySelector('meta[name="description"]')?.content, content.description);
assert.match(document.querySelector('meta[name="robots"]')?.content || '', /^index,follow/);
assert.equal(document.querySelector('meta[property="og:url"]')?.content, canonical);
assert.equal(document.querySelector('meta[property="og:title"]')?.content, document.title);
assert.equal(document.querySelector('meta[name="twitter:description"]')?.content, content.description);

const main = document.querySelector('#root main');
assert.ok(main, 'homepage product content must exist before JavaScript runs');
const media = JSON.parse(await readFile(new URL('../src/home-media.json', import.meta.url), 'utf8'));
assert.equal(main.querySelector('.home-phone-image')?.getAttribute('src'), media.phoneImage, 'prerender uses the same R2 image as the interactive homepage');
assert.equal(new URL(media.phoneImage).origin, 'https://img.okkpwa.com', 'homepage image is served by the configured R2 public host');
assert.equal(main.querySelectorAll('h1').length, 1);
assert.equal(main.querySelector('h1')?.textContent, content.hero.lines.join(''));
assert.ok(main.textContent.includes(content.hero.description));
for (const item of content.growth.items) {
  const article = main.querySelector(`#growth-${item.id}`);
  assert.equal(article?.querySelector('h3')?.textContent, item.title);
  assert.ok(article.textContent.includes(item.description), `growth copy missing: ${item.id}`);
  if (item.metric) assert.ok(article.textContent.includes(item.metricLabel), 'percentage labels must remain visible in prerendered copy');
}
for (const item of [
  { id: 'apk', title: content.paths.apkHeading, description: content.paths.apkDescription, tags: ['APK · IPA', '版本管理', '下载统计'] },
  { id: 'pwa', title: content.paths.pwaTitle, description: content.paths.pwaSummary, tags: ['无需下载 APK', '独立桌面入口', 'Web Push'] },
]) {
  const article = main.querySelector(`article[id="${item.id}"]`);
  assert.equal(article?.querySelector('h3')?.textContent, item.title);
  assert.equal(article?.querySelector('p')?.textContent, item.description);
  assert.deepEqual([...article.querySelectorAll('li')].map(node => node.textContent), item.tags);
}
assert.deepEqual([...main.querySelectorAll('.home-faq-list summary')].map(node => node.textContent), content.faq.items.map(item => item.question));
assert.deepEqual([...main.querySelectorAll('.home-faq-list details p')].map(node => node.textContent), content.faq.items.map(item => item.answer));
assert.equal(main.querySelectorAll('[hidden], [aria-hidden="true"]').length, 0, 'SEO copy is normal visible product content');
for (const keyword of ['APK 分发', 'PWA', 'DIY 落地页', '模板库', 'Cloudflare R2', '外部浏览器跳转', '回流页', 'Web Push', '应用分流', '智能斗篷', '下载统计']) assert.ok(main.textContent.includes(keyword), `missing visible product topic: ${keyword}`);
assert.equal(main.querySelectorAll('form, input').length, 0, 'initial HTML must contain public marketing content only');
assert.ok(!html.includes('HOME_SEO_HEAD') && !html.includes('HOME_PRERENDER'), 'build placeholders must be replaced');

const data = JSON.parse(document.querySelector('#home-structured-data')?.textContent || '{}');
assert.deepEqual(data['@graph'].map(item => item['@type']), ['Organization', 'WebSite', 'SoftwareApplication']);
for (const entity of data['@graph']) {
  assert.equal(entity.url, canonical);
  assert.equal(entity.name, 'D1 Apk');
  assert.ok(!entity.offers && !entity.aggregateRating && !entity.review, 'structured data must not invent prices or reviews');
}
assert.equal(data['@graph'][2].description, content.description);
assert.deepEqual(data['@graph'][2].featureList, [content.paths.apkHeading, content.paths.pwaTitle, ...content.growth.items.map(item => item.title)]);
for (const feature of data['@graph'][2].featureList) assert.ok(main.textContent.includes(feature), 'structured features must also be visible on the page');
console.log('Built homepage SEO check passed: public metadata, visible product content and all FAQs are readable without JavaScript.');
