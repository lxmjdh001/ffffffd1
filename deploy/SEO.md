# Google / Bing 首页 SEO 与关键词（2026-10-26）

唯一收录地址为 `https://d1apk.com/`。Google 和 Bing 使用同一份真实产品内容，不按爬虫身份替换页面。关键词按当前已经具备的功能整理，是选题和内容覆盖目标，不代表经过搜索量工具验证的热度排名。

## 搜索标题与描述

标题：**D1 Apk · APK 分发与 PWA 制作平台 · DIY 落地页与智能斗篷**

描述：D1 Apk 一站式 APK 分发与 PWA 制作平台。粘贴 HTML 创建 DIY 落地页，收藏模板并绑定 APK/PWA，配合智能斗篷、外部浏览器跳转、回流页、广告回传和下载统计，按下载与安装用量计费。

标题、描述、首屏介绍、产品标题和 FAQ 共用 `web/src/home-content.json`，保留九语言翻译。首页主视觉标题继续使用“让每一次投放，更快抵达用户”，首屏说明自然补充 APK 分发、网页转 PWA、DIY 落地页等业务词。

## 关键词布局

| 优先级 / 功能 | 中文主题与长尾词 | 英文主题 | 内容位置 |
| --- | --- | --- | --- |
| 核心：APK 分发 | APK 分发平台、APK 下载分发、安卓安装包托管、应用下载链接 | APK distribution platform, Android APK hosting | title、description、首屏、APK 产品卡 |
| 核心：PWA 制作 | PWA 制作平台、网页转 PWA、网站生成 PWA、可安装网页应用 | PWA builder, website to PWA, installable web app | title、description、PWA 产品卡、PWA FAQ |
| 核心：DIY 落地页 | DIY 落地页、HTML 落地页制作、APK 下载落地页、PWA 安装落地页 | HTML landing page builder, APK landing page, PWA landing page | title、description、DIY 功能卡、DIY FAQ |
| 模板库与编辑 | 落地页模板库、收藏落地页模板、编辑标题图片和按钮、R2 图片上传 | landing page templates, landing page editor | DIY 功能卡、DIY FAQ |
| 智能斗篷 | 智能斗篷、Cloak 投放工具、地区设备语言访问规则、投放计划数据 | smart cloak, campaign access rules | title、description、斗篷功能卡 |
| 页面与文本混淆 | 页面代码混淆、页面防关联、文本混淆、品牌文本展示 | page code obfuscation, text obfuscation | 代码混淆、文本混淆功能卡 |
| 外部浏览器跳转 | TikTok 外部浏览器跳转、Facebook 外部浏览器打开、应用内浏览器跳转 | TikTok external browser, Facebook in-app browser redirect | 外部浏览器功能卡、PWA FAQ |
| 回流页 | 回流页、返回弹窗、二次曝光、回流页自定义图片视频 | return page, repeat exposure, return-page popup | description、回流页功能卡、PWA FAQ |
| Cloudflare 加速 | Cloudflare R2 资源加速、CDN 边缘缓存、全球静态资源加速 | Cloudflare R2 CDN, static asset delivery | Cloudflare 功能卡 |
| 广告与统计 | Meta Facebook 广告回传、TikTok 广告转化回传、APK 下载统计 | ad conversion tracking, download analytics | description、首页功能摘要 |
| 分流与域名 | 应用分流、多域名下载链接、自定义下载域名、域名安全检测 | traffic routing, custom download domain, domain safety checks | 产品说明、计费免费功能、FAQ |
| 使用与计费 | APK 按下载计费、PWA 按安装计费、应用分发平台价格 | pay-per-download, pay-per-install, APK PWA pricing | 定价区、计费 FAQ |

品牌检索目标包括 **D1 Apk、D1APK、D1 APK 分发、D1 PWA 制作、D1 DIY 落地页**。不加入其他公司的品牌、未实现的功能、虚构的认证或搜索量。

## 已落地的技术设置

- 初始 HTML 预渲染首屏、APK/PWA 产品介绍、七个功能卡和全部 FAQ，禁用 JavaScript 也能读取。预渲染与 React 共用正文，段落顺序一致。
- title、description、canonical、Open Graph、Twitter 信息同步更新。
- Organization / WebSite / SoftwareApplication JSON-LD 使用真实品牌和功能；featureList 来自页面可见的产品与功能标题，不编造评分、评论或价格。
- Google 不使用 meta keywords；关键词写入可读的标题、描述和正文，避免机械重复、隐藏词堆砌和爬虫专用页面。
- /robots.txt 和 /sitemap.xml 仅在控制台主域名公开；sitemap 只列首页，不列登录、控制台、邀请参数、客户 APK/PWA 下载页、分流或斗篷链接。
- 私有 HashRouter 路由保持 noindex,nofollow。robots 不是鉴权手段。
- 构建时 SEO_PUBLIC_URL 与生产 APP_URL 均应为 https://d1apk.com；canonical 不接受路径、查询参数、片段或凭证。

`npm --prefix web run build` 检查最终 HTML 中的元数据、关键词主题、全部 FAQ、canonical，以及结构化功能与可见内容的一致性。服务端测试继续检查 robots、sitemap 和主域名限制。

## 上线后索引管理

在已验证该域名的 Google Search Console 和 Bing Webmaster Tools 中提交 `https://d1apk.com/sitemap.xml`，再检查首页 URL 的抓取与索引状态。站长平台认领需要对应账号权限；部署不等于已经提交站长平台，也不保证收录时间或排名。项目没有写入虚构的验证码、IndexNow key 或提交结果。

## 官方依据

- [Google 支持的 meta 标签](https://developers.google.com/search/docs/crawling-indexing/special-tags)：description 与索引控制，以及不使用 meta keywords 的说明。
- [Google 标题链接](https://developers.google.com/search/docs/appearance/title-link)：描述性标题、页面主标题与实际内容的一致性。
- [Google JavaScript SEO](https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics)：可读取的正文、元数据和预渲染。
- [Bing Webmaster Guidelines](https://www.bing.com/webmasters/help/webmaster-guidelines-30fba23a)：可发现的真实内容、页面结构、清晰的标题与避免关键词堆砌。
