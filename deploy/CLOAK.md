# 斗篷访问规则

控制台可以建立多组独立规则，选择当前账号的分流链接，或填写完整的 HTTP/HTTPS 目标地址。每组规则获得稳定的 `/c/<slug>` 入口。删除规则后入口返回 404；关闭“拦截规则”会保留入口并直接进入目标页。账号停用后其入口返回 404。

## 匹配与跳转

- 语言、UA、国家、设备中的任一拦截条件命中，进入安全页。未选择的条件不限制访问。
- 安全页 URL 可选，保存时会去除首尾空白。不填写时，需进入安全页的访问返回真正的 HTTP 404，显示独立的英文 “Account not found” 页面及 “Please contact customer support for assistance.” 说明；正常通过规则的访问仍跳转目标页。后台配置了有效客服邮箱时显示 “Contact support” 邮件链接，未配置时不显示按钮。
- 语言读取 `Accept-Language` 中 `q > 0` 的语言，`zh` 同时匹配 `zh-CN` 等子标签；`zh-CN` 不匹配 `zh-TW`。
- UA 关键词按不区分大小写的子串匹配，最多 100 项，每项最多 200 个字符，保存时清理控制字符。
- 国家为黑名单，只有选中的国家被拦截。无法取得可信地区信息时统一为 `XX`（未知地区），只有选中“未知地区”才拦截；Cloudflare 的 Tor 标记 `T1` 对应独立的“Tor 网络”选项。
- “广告允许”读取入口顶层查询参数：TikTok `ttclid`、Facebook `fbclid`、Google `gclid`、Bigo `bbg`。选择多个广告平台时，任一参数具有非空值即可通过广告条件；选择零个平台时不限制。空参数或嵌套 URL 中的同名参数不会通过。该条件依据访客提交的点击参数，不是平台签名认证。
- 电脑、平板、手机独立匹配；每类可以选择系统或“全部系统”。同一设备未选择系统时不拦截。
- 目标页保留原有查询参数，并以入口查询参数覆盖同名项；安全页只使用配置的 URL，不透传入口参数。URL 片段保持不变。
- 选择分流链接时直接复用分流的随机/顺序、付费风险过滤和事务游标，不产生额外 `/r/` 跳转。分流停用、删除或没有可用目标时进入安全页。分流标识更改后，需要重新选择对应分流。
- 目标页及已填写的安全页使用浏览器重定向，D1 不在服务器抓取目标页或安全页。禁止将本站、官网的 www/根域名别名或已登记绑定域名的斗篷入口配置为目标页/安全页，访问时也会再次检查循环。

语言、UA、设备和点击参数都来自客户端；浏览器的桌面模式和 UA 变更可能影响设备识别。

## 可信国家来源

国家识别不调用第三方 IP 接口，使用 Cloudflare 的 `CF-IPCountry`。在 Cloudflare 中启用 **Add visitor location headers**，并保证相关域名开启代理。没有经 Cloudflare 转发、没有地区头或解析失败时，地区为 `XX`。

默认可信边缘网段来自 Cloudflare 官方 [IPv4 列表](https://www.cloudflare.com/ips-v4) 和 [IPv6 列表](https://www.cloudflare.com/ips-v6)，核对时间为 2026-10-24，共 22 段。运营者应随官方调整更新配置：

```dotenv
# 空值：使用内置 Cloudflare 网段；none：明确禁用国家识别。
CLOAK_COUNTRY_PROXY_CIDRS=

# 只写可信内部反向代理网段，不要将任意公网客户端列为可信代理。
TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
```

应用仅在 socket 对端属于权威边缘网段，或 socket 对端属于 `TRUSTED_PROXY_CIDRS` 且逆向检查 `X-Forwarded-For` 得到的首个非内部代理属于权威边缘网段时，接受地区头。直连来源提供的地区头或伪造的转发链不会获得信任。

内部反向代理必须追加其真实 socket 对端，例如 Nginx 使用 `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`；不能盲目转发或用外部传入的值替代真实对端。边缘应覆盖访客提供的 `CF-IPCountry`。自建权威边缘可用 `CLOAK_COUNTRY_PROXY_CIDRS` 显式覆盖默认值，但必须同样保证覆盖地区头。

列表 API 的 `country_detection_configured` 表示可用的权威边缘网段已配置，不代表每个访问请求都经过该边缘。

## API

`GET /api/cloaks` 返回 `{data: Cloak[], country_detection_configured: boolean}`。`POST /api/cloaks` 新建、`PUT /api/cloaks/{id}` 更新，成功返回该 `Cloak`，包括只读的 `id`、`slug`、`entry_url`、`created_at`、`updated_at`。`DELETE /api/cloaks/{id}` 返回 `{ok: true}`。修改操作必须提供登录会话和 CSRF token，所有记录及分流引用按账号隔离。

可编辑字段：

```json
{
  "name": "访问规则",
  "enabled": true,
  "target_type": "url",
  "target_url": "https://example.com/landing",
  "traffic_pool_slug": "",
  "safe_url": "https://example.com/help",
  "blocked_languages": ["zh"],
  "blocked_uas": ["ExampleCrawler"],
  "blocked_countries": ["CN", "XX"],
  "allowed_platforms": ["tiktok", "facebook"],
  "blocked_devices": [{"device": "desktop", "systems": ["all"]}]
}
```

`safe_url` 可省略或传空字符串，空白会被归一化为空；非空值仍须为完整 HTTP/HTTPS URL，并通过循环检测。既有非空但无效的安全页配置在需要回退时仍返回 503。

`target_type` 也可为 `traffic_pool`，这时填写当前账号的 `traffic_pool_slug`，忽略并清空 `target_url`。设备为 `desktop`、`tablet`、`mobile`。电脑系统可选 `windows`、`macos`、`linux`、`chromeos`、`other`；平板和手机可选 `ios`、`android`、`harmonyos`、`windows`、`other`；各设备均支持 `all`。

公开入口支持 `GET` 和 `HEAD`：目标页与已配置安全页返回不缓存的 302，未配置安全页的回退返回 `text/html; charset=utf-8` 的 404，同时设置 `Cache-Control: no-store` 和 `X-Robots-Tag: noindex, nofollow`。HEAD 使用相同状态与响应头、不返回正文。默认 404 页面不包含账号信息、入口参数、目标地址或规则内容。绑定域名也可使用 `/c/<slug>`，仅能访问该域名所属账号的规则。

现有迁移启动流程会自动创建 `cloaks` 表，不改写既有账号、分流配置、订单或余额。
