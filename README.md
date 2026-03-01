# V2bX

[![](https://img.shields.io/badge/TgChat-UnOfficialV2Board%E4%BA%A4%E6%B5%81%E7%BE%A4-green)](https://t.me/unofficialV2board)
[![](https://img.shields.io/badge/TgChat-YuzukiProjects%E4%BA%A4%E6%B5%81%E7%BE%A4-blue)](https://t.me/YuzukiProjects)

A V2board node server based on multi core, modified from XrayR.  
一个基于多种内核的V2board节点服务端，修改自XrayR，支持V2ay,Trojan,Shadowsocks协议。

**注意： 本项目需要搭配[修改版V2board](https://github.com/wyx2685/v2board)**

## 特点

* 永久开源且免费。
* 支持Vmess/Vless, Trojan， Shadowsocks, Hysteria1/2多种协议。
* 支持Vless和XTLS等新特性。
* Xray内核已支持 `xhttp`/`splithttp` 传输（可用于 VLESS + REALITY）。
* 支持单实例对接多节点，无需重复启动。
* 支持限制在线IP。
* 支持限制Tcp连接数。
* 支持节点端口级别、用户级别限速。
* 配置简单明了。
* 修改配置自动重启实例。
* 支持多种内核，易扩展。
* 支持条件编译，可仅编译需要的内核。

## 功能介绍

| 功能        | v2ray | trojan | shadowsocks | hysteria1/2 |
|-----------|-------|--------|-------------|----------|
| 自动申请tls证书 | √     | √      | √           | √        |
| 自动续签tls证书 | √     | √      | √           | √        |
| 在线人数统计    | √     | √      | √           | √        |
| 审计规则      | √     | √      | √           | √         |
| 自定义DNS    | √     | √      | √           | √        |
| 在线IP数限制   | √     | √      | √           | √        |
| 连接数限制     | √     | √      | √           | √         |
| 跨节点IP数限制  |√      |√       |√            |√          |
| 按照用户限速    | √     | √      | √           | √         |
| 动态限速(未测试) | √     | √      | √           | √         |

## TODO

- [ ] 重新实现动态限速
- [ ] 重新实现在线IP同步（跨节点在线IP限制）
- [ ] 完善使用文档

## 软件安装

### 一键安装

```
wget -N https://raw.githubusercontent.com/yamatu/yav2bx/main/install.sh && bash install.sh

# 安装指定版本
bash install.sh v1.0.2

# 系统 apt/dpkg 异常时，跳过基础依赖安装
V2BX_SKIP_BASE_INSTALL=1 bash install.sh v1.0.2

# 或者使用 curl 管道方式
bash <(curl -Ls https://raw.githubusercontent.com/yamatu/yav2bx/main/install.sh) v1.0.2
```

安装脚本会自动写入 systemd 服务，并保留你已有的 `/etc/V2bX/config.json`。首次安装会额外放置 XHTTP 示例配置：

安装完成后，执行 `v2bx` 会进入数字菜单（0/1/2...）管理模式，可直接进行安装/更新/重启/日志/配置生成等操作。

若仓库暂未发布 Release，脚本会自动切换为源码编译安装（默认包含 xray 内核编译标签），同样可用 xhttp。

- `/etc/V2bX/config_xhttp_reality.json`
- `/etc/V2bX/xhttp_template.conf`

### 配置文件位置

- 主配置：`/etc/V2bX/config.json`
- DNS 配置：`/etc/V2bX/dns.json`
- 路由配置：`/etc/V2bX/route.json`
- 自定义出站：`/etc/V2bX/custom_outbound.json`
- 自定义入站：`/etc/V2bX/custom_inbound.json`

可通过 `v2bx` 菜单中的 `0` 编辑主配置，或 `13` 使用向导新建/重建配置。
`13` 向导已支持 `vless + xhttp` 预设，并可选择 `reality`（默认）或 `tls`。

### XHTTP 使用说明

- 项目已在 `core/xray/inbound.go` 中处理 `xhttp`/`splithttp` 入站网络类型。
- 面板节点请使用 `vless` 协议并将 `network` 设为 `xhttp`。
- `network_settings` 可参考仓库内的 `xhttp配置模板.conf`。
- 完整示例可参考 `example/config_xhttp_reality.json`。

### 手动安装

[手动安装教程](https://v2bx.v-50.me/v2bx/v2bx-xia-zai-he-an-zhuang/install/manual)

## 构建
``` bash
# 默认构建 xray 内核（推荐，支持 xhttp）
go build -v -o ./V2bX -tags "xray with_reality_server with_quic with_grpc with_utls with_wireguard with_acme" -trimpath -ldflags "-s -w -buildid="

# 如需自定义 tags，可在安装脚本中设置：
# V2BX_BUILD_TAGS="xray sing hysteria2 with_reality_server with_quic with_grpc with_utls with_wireguard with_acme" bash install.sh
```

## 配置文件及详细使用教程

[详细使用教程](https://v2bx.v-50.me/)

## 免责声明

* 此项目用于本人自用，因此本人不能保证向后兼容性。
* 由于本人能力有限，不能保证所有功能的可用性，如果出现问题请在Issues反馈。
* 本人不对任何人使用本项目造成的任何后果承担责任。
* 本人比较多变，因此本项目可能会随想法或思路的变动随性更改项目结构或大规模重构代码，若不能接受请勿使用。

## Thanks

* [Project X](https://github.com/XTLS/)
* [V2Fly](https://github.com/v2fly)
* [VNet-V2ray](https://github.com/ProxyPanel/VNet-V2ray)
* [Air-Universe](https://github.com/crossfw/Air-Universe)
* [XrayR](https://github.com/XrayR/XrayR)
* [sing-box](https://github.com/SagerNet/sing-box)
* [V2bX](https://github.com/wyx2685/V2bX)

## Stars 增长记录

[![Stargazers over time](https://starchart.cc/Fearless743/V2bX.svg)](https://starchart.cc/Fearless743/V2bX)
