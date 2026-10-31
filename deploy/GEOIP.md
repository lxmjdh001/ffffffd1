# 国家识别与本地 GeoIP 兜底

请求先校验 Cloudflare 来源和内部代理链。可信 `CF-IPCountry` 优先（保留
`T1` 的原有含义）；缺失、无效或为 `XX` 时，使用已校验的真实访客 IP
查询本地 Country MMDB。灰云和直连同样使用本地库。缺少可靠访客 IP、
私网/回环地址、数据库不可用或没有匹配记录时仍返回 `XX`。

不会查询 Cloudflare 后台的代理开关。每次请求只进行本地网络段比对和
数据库查询，不向外部服务发送客户 IP。Country 识别统一用于斗篷规则、
APK/PWA/分流访问记录和推送订阅的国家信息；历史访问记录不自动改写。

ASN MMDB 提供网络组织与自治系统编号，不能代替 Country MMDB。本次不
增加任何按 ASN 拦截或修改广告回传、下载计费的规则。

## 文件与配置

默认路径（相对 DATA_DIR）：

- `geoip/GeoLite2-Country.mmdb`
- `geoip/GeoLite2-ASN.mmdb`

可以分别用 `GEOIP_COUNTRY_DB`、`GEOIP_ASN_DB` 覆盖路径。设为 `none` 可
关闭对应本地库。默认文件不存在时兼容原有部署；显式路径有误或文件
损坏时启动日志会报告，服务继续使用可信国家头。

启动只打开并验证一次数据库，多个请求复用读取器。数据库不会放到 R2，
不会被打包进前端或作为公开下载文件。

## 下载、验证、更新

数据来自用户指定的 https://github.com/P3TERX/GeoLite.mmdb ，包含 MaxMind
GeoLite2 数据，需保留来源和 MaxMind 的许可/归属信息。

首次接入可先下载到暂存目录，不影响线上进程：

```sh
python3 deploy/geoip-download.py --directory /var/lib/d1apk/geoip
```

用已经支持 GeoIP 的待发布程序验证并安装暂存文件（旧版本程序不支持此
命令，不可将其当成验证器）：

```sh
python3 deploy/geoip-download.py --directory /var/lib/d1apk/geoip \
  --staged /var/lib/d1apk/geoip/incoming-实际目录 \
  --validator /opt/d1apk/releases/待发布版本/bin/d1apk --activate
```

更新时也可以一次完成下载、MMDB 类型和完整性验证及原子替换：

```sh
python3 deploy/geoip-download.py --validator /opt/d1apk/current/bin/d1apk --activate
systemctl kill --kill-who=main --signal=HUP d1apk
```

更新脚本保留旧文件，下载失败不会替换线上数据库。运行中的读取器继续
使用旧文件映射；HUP 验证新库后才交换读取器。更新失败保留旧读取器。
本次未安装自动更新定时任务。

## 独立诊断

```sh
/opt/d1apk/current/bin/d1apk geoip-check \
  -country /var/lib/d1apk/geoip/GeoLite2-Country.mmdb \
  -asn /var/lib/d1apk/geoip/GeoLite2-ASN.mmdb 8.8.8.8 2001:4860:4860::8888
```

诊断命令在读取应用环境、连接数据库或启动服务之前返回。它输出库类型、
构建日期、指定 IP 的国家/ASN，以及预热后 Country 查询的平均耗时；该
平均值不是完整 HTTP 请求的延迟或冷启动指标。
