# 详情页静态 JS 放 R2

迁移 APK/PWA 详情页使用的静态 JS，以及明确指定的两张模板图片：
`GooglePlaybestapp-right.png`、`age/age0.png`。其他图片、CSS、HTML、manifest、
应用动态配置和同源 Service Worker 入口不迁移。使用已有 `PAGE_IMAGE_R2_*` 配置。

发布包含五个 JS：Google 模板、自定义模板、APK 客户端、PWA 客户端和预览适配。
模板原有共享依赖合并进两个模板 JS，作为 `defer` 普通脚本加载，执行顺序保持
在动态配置和客户端之后，不需要调整共享桶 CORS。两张指定图片按内容哈希发布到
`d1apk/landing-images/<内容哈希>/<原文件路径>`，发布时替换模板 JS 中的对应地址。
CSS 和其他图片地址仍指向原站。图片同样缓存一年；旧版本及原站路径继续保留。

对象路径：`d1apk/landing-js/<内容哈希>/<文件名>.js`。
Content-Type 为 JavaScript，Cache-Control 为 `public, max-age=31536000, immutable`。
任何脚本内容变化都会生成新的发布目录，不覆盖旧版本，避免缓存更新不及时。
旧发布保留，不自动清理，避免影响已打开页面或其他部署。

更新原模板时运行：

```sh
npm --prefix pwa-templates ci
npm --prefix pwa-templates run build
```

`scripts/build-classic.mjs` 从已经编译的原模板 JS 再生成普通脚本；生成过程
不复制图片，也不生成 CSS。提交 `templates/landing-classic/*.js`，以便干净
checkout 直接 Go 编译。原模板清单与普通脚本不匹配时，发布拒绝启用旧脚本。

启动时自动发布并校验所有 JS 的公开响应内容，成功后才切换页面引用。
也可加载私有环境文件后运行 `bin/d1apk publish-landing-js` 预先发布。
R2 未配置或发布失败时继续使用原站脚本，不因 R2 上传失败阻止服务启动。

仅公开详情页及模板预览 CSP 允许此发布目录；控制台/API 的脚本来源不放宽。
后端保留原脚本路由供旧页面使用，应用配置继续 no-store，Service Worker
继续同源 no-cache，下载权限和安装/推送逻辑不变。
