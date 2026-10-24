# 首页 SEO 与关键词

公开首页以 `https://d1apk.com/` 为唯一收录地址。首页文案围绕 APK 分发、安卓安装包托管、应用分流、随机/顺序分流、多域名管理、斗篷 Cloak、下载落地页、下载统计、域名风险检测与广告回传展开。广告回传只描述已支持的 Meta / Facebook、TikTok，以及完整下载、打开应用和注册成功事件。

`web/src/home-content.json` 是标题、描述、产品说明和 FAQ 的共享内容源。Vite 将其中的正文预渲染到初始 HTML，浏览器与爬虫收到相同内容，禁用 JavaScript 仍可阅读核心产品介绍。React 首页继续使用同一份文案，套餐价格由线上配置加载，不在静态正文中写死。

构建包含 title、description、canonical、Open Graph、Twitter 元信息和 Organization / WebSite / SoftwareApplication JSON-LD。没有虚构评分、评论或价格。默认主域名为 D1 官网；其他部署域名须在构建时指定 `SEO_PUBLIC_URL=https://your-domain.example`，并与服务端 `APP_URL` 一致。仅接受不含路径、查询、片段及凭证的 HTTP(S) origin。

服务端提供 `/robots.txt` 和 `/sitemap.xml`，仅在控制台主域名可用。站点地图只包含首页，不包含邀请参数、HashRouter 后台路由、客户下载页、文件、分流或斗篷链接。接口与下载入口在 robots 中排除，静态资源保持可抓取。登录及控制台的前端路由切换会更新 noindex；原有鉴权继续负责访问控制，robots 不是安全边界。

`npm --prefix web run build` 会检查最终 HTML：不执行 JavaScript，直接验证正文、全部 FAQ、canonical、元数据和结构化数据。发布后再检查公开首页、robots、站点地图及手机布局。

上线本身不代表已完成 Google Search Console 或 Bing Webmaster Tools 的站点认领。可以在拥有该域名权限的站长账号中认领网站并提交 `https://d1apk.com/sitemap.xml`；实际收录和排名由搜索引擎决定。未添加虚假的站长验证码。

## 调研依据（2026-10-24）

- [蒲公英官方分发页](https://www.pgyer.com/app/distribution)：应用分发、安装包托管、下载链接、版本管理、下载明细等主题词。
- [蒲公英下载页设置](https://api.pgyer.com/doc/view/app_setting)：下载页外观、版本信息与下载入口。
- [LinkNova](https://pathgo.org/) 与 [FastRedir](https://www.fastredir.cn/link)：链接分流、自定义域名、设备与访问统计的常见表达。
- [Google JavaScript SEO](https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics)：预渲染、原始 HTML 元数据及 URL 原则。
- [Google 支持的 meta 标签](https://developers.google.com/search/docs/crawling-indexing/special-tags)：Google 不使用 meta keywords。
- [Bing Webmaster Guidelines](https://www.bing.com/webmasters/help/webmaster-guidelines-30fba23a)：可发现的正文、canonical 站点地图与自然的关键词布局。

同行用词仅用来确定业务主题，不代表搜索量排名；没有复制同行文案、品牌、客户数量或未实现的功能。关键词融入可读的标题、功能与问题答案，避免隐藏文字和机械重复。
