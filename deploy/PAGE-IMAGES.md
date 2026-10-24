# 详情页图片（R2）

图标和截图上传到 R2，详情页数据库只保存公开 HTTPS URL。浏览器先校验、压缩
PNG/JPEG/WebP，经 D1 同源接口上传；不需要修改共享桶的 CORS 或放宽页面 CSP。
旧的内嵌图片和手工填写的 HTTPS 图片地址继续兼容，不会自动迁移。

## 配置

在 D1 的私有环境文件中填写以下配置，本地为 `.env.development`，线上为
`/etc/d1apk/app.env`。可复用 PWA 的账号、桶和公开域名，但不要复制其他配置。

| D1 环境变量 | PWA 对应配置 |
| --- | --- |
| `PAGE_IMAGE_R2_ENDPOINT` | `S3_ENDPOINT`（包含 `https://`） |
| `PAGE_IMAGE_R2_ACCESS_KEY` | `S3_ACCESS_KEY_ID` |
| `PAGE_IMAGE_R2_SECRET_KEY` | `S3_SECRET_ACCESS_KEY` |
| `PAGE_IMAGE_R2_BUCKET` | `S3_BUCKET` |
| `PAGE_IMAGE_R2_PUBLIC_URL` | `S3_PUBLIC_DOMAIN`（公开 HTTPS 域名） |

这组配置与 D1 的 `S3_*` **独立**，不要改动安装包的存储配置。五项要么全部
填写，要么全部留空；留空时仍可编辑详情页、填写 HTTPS 图片，但不能上传新图片。
密钥仅限 D1 后端使用，环境文件权限保持 `0600`，不要提交 Git。

## 隔离与清理

对象路径为 `d1apk/page-images/<环境标识>/<应用ID>/<随机ID>.jpg`。
环境标识从 R2 端点、桶和 D1 `APP_URL` 派生，隔离本地与线上环境。改变这三项时
不会删除旧环境的文件；如需清理旧环境，应单独核实对象后操作。

D1 只清理自身数据库登记、且符合当前环境前缀的图片，从不列举共享桶、删除 PWA
路径或删除用户粘贴的外部图片。删除应用时清理其上传图片；失败保留对象登记以供
后台重试，应用本身仍为物理删除。后台每日清理超过 24 小时且未发布的上传文件，
已发布的图片保留。长时间未保存的草稿如果图片过期，需要重新上传。

新接口 `POST /api/apps/:id/page-images` 要求登录、应用归属及 CSRF，限制体积、
图片尺寸、上传频率和并发，并在后端重新编码 JPEG，不信任文件名或声明的 MIME。

## 验证

普通 `go test ./...` 不访问真实 R2；使用 TLS 的 S3 协议测试验证签名、上传、
权限、公开页渲染及隔离清理。显式进行真实 R2 验证时，可运行：

```sh
set -a
source .env.development
set +a
D1_R2_LIVE_TEST=1 go test ./internal/platform -run '^TestPageImageR2Live$' -count=1 -v
```

该测试使用独立临时数据库，只新增一个随机 D1 测试应用的图片并在结束时删除。
若报告清理失败，按测试输出的应用 ID 定位本次独立目录，不要清扫共享桶。
