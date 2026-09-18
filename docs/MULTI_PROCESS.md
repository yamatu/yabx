# 多进程模式（每节点一个进程）

V2bX 默认把 `config.json` 里所有节点跑在**一个进程**里。这样有两个副作用：

1. 任意一个节点启动失败（面板连不上、证书读取失败、用户列表为空……），`cmd/server.go` 会直接 `os.Exit(1)`，**其余节点一起退出**；
2. 面板下发的 DNS 配置变化会写进 `DnsConfigPath` 指向的文件，`conf.Watch` 检测到文件变化后**重启整个进程**，于是所有节点都会瞬断一次。

多进程模式把每个节点拆成独立的 systemd 实例，节点之间互不影响。**默认安装和默认行为完全不变**，迁移是可选操作，随时可以回滚。

## 命令一览

```bash
v2bx multi                  # 帮助
v2bx multi status           # 模式 + 每个实例的状态和自启状态
v2bx multi migrate          # 拆分配置并切换到多进程模式（会停掉单进程 V2bX）
v2bx multi split            # 只生成每节点配置，不改动 systemd
v2bx multi reload           # 按当前 config.json 重新拆分，只重启正在运行的实例
v2bx multi rollback         # 回滚到单进程模式（保留 nodes/ 目录）
v2bx multi menu             # 交互式菜单（与主菜单第 16 项相同）

v2bx start|stop|restart|status|log [节点名]   # 单独控制某个实例
```

不带节点名时，`start/stop/restart/status/log/enable/disable` 会自动作用于**所有实例**（多进程模式）或 `V2bX` 服务（单进程模式）。

## 文件布局

拆分**不会修改** `/etc/V2bX/config.json`，它只是源文件：

```
/etc/V2bX/
├── config.json          # 保持不变，仍然是唯一需要维护的配置
├── dns.json             # 全局默认 DNS（会被抄一份给每个节点）
├── route.json           # 所有节点共用
└── nodes/
    ├── 45678.json       # 节点 45678 的完整配置（单节点）
    ├── 45679.json
    ├── 45680.json
    ├── dns/
    │   ├── dns_45678.json   # 每个节点独立的 DNS 文件（必须独立）
    │   └── ...
    └── log/
        └── V2bX-45678.log   # 仅当原配置设置了 Log.Output 时才有
```

- 实例名 = `nodes/` 下配置文件的文件名（去掉 `.json`），默认取节点的 `Name`；
- `nodes/*.json` 就是实例列表，`dns/` 和 `log/` 是子目录，不会被当成实例；
- 每个实例的 `DnsConfigPath` 都会被改写成 `nodes/dns/dns_<实例名>.json`，并复制原文件内容。**这一步不能省**：面板每次下发 DNS 都会重写这个文件，各节点共用一个文件时会互相触发全进程重启；
- 如果原配置有 `Log.Output`，会被改写成 `nodes/log/<原名>-<实例名>.log`，因为 lumberjack 没有跨进程的文件锁，多个进程写同一个日志文件会出现覆盖和错乱；
- 其余字段（`CertConfig`、`ApiKey`、`Timeout`、未知字段……）原样保留，JSON5 注释和 `Include` 形式的配置也能正常拆分。

## 迁移

```bash
v2bx update          # 确保二进制和新版 systemd 模板都在
v2bx multi split     # 可选：先看看生成的配置对不对
v2bx multi migrate   # 确认后切换（会 stop + disable V2bX，再启动各实例）
v2bx multi status
```

`migrate` 做这些事：

1. 生成 `nodes/`（已存在时会询问，或加 `-f` / `--force` 覆盖）；
2. `systemctl stop V2bX && systemctl disable V2bX`；
3. 对每个配置执行 `systemctl enable --now v2bx@<实例名>`；
4. 用 `systemctl list-unit-files` 检查是否有已启用但配置已删除的实例。

实例模板单元 `v2bx@.service` 的 `ExecStart` 是
`/usr/local/V2bX/V2bX server -c /etc/V2bX/nodes/%i.json`，
并带 `Restart=on-failure` 和 `StartLimitIntervalSec=0`（节点之间已经隔离，单个节点重启循环不会影响别人，比"启动几次后永久死掉"更好）。

## 回滚

```bash
v2bx multi rollback
```

它会 `systemctl disable --now v2bx@<实例名>`（每个实例），然后 `systemctl enable --now V2bX`。`nodes/` 目录会保留，想再切回来直接 `v2bx multi migrate` 即可。

也可以从命令行手动回滚：

```bash
systemctl disable --now 'v2bx@45678' 'v2bx@45679' 'v2bx@45680'
systemctl enable --now V2bX
```

## 修改配置

多进程模式下 `v2bx edit` 编辑的仍然是 `/etc/V2bX/config.json`，**但实例读的是 `nodes/*.json`**，改完需要重新拆分：

```bash
v2bx multi reload    # 重新拆分并重启所有正在运行的实例
```

菜单会提示这一点。

## 注意事项

- **ACME HTTP 模式**：每个进程都会去占 80 端口去签发/续期证书，节点多了会互相抢端口。多节点建议用 DNS 模式或者共用证书文件（`CertMode: file`）。`CertMode: none`（由面板下发证书）不受影响。
- **面板压力**：每个节点一个进程 = 每个进程独立上报状态、拉取用户列表，面板的请求数变成 N 倍。面板比较弱时请分批迁移。
- **DNS 文件**：不要手动把多个节点的 `DnsConfigPath` 指回同一个文件，会退化成"一个节点更新 DNS，全体重启"。
- **`v2bx update` 之后新增的实例模板**：老版本装的系统里没有 `v2bx@.service`，`v2bx multi migrate` 会提示先执行 `v2bx update`。
- 单进程模式依旧可以通过 `enable/disable` 控制开机自启，多进程模式下这两个动作会作用到所有实例。

## 任务抖动（同一进程内的优化）

`common/task` 的周期性任务（上报流量、拉取用户等）以前是固定间隔，多个节点在同一个进程里会在同一秒一起发起请求，形成周期性尖峰。现在每个任务的实际间隔在 `Interval ±10%` 内随机（`task.Jitter`，均值不变），把尖峰摊开。单进程模式下同样生效。

## 排障

```bash
v2bx multi status                      # 哪些实例在跑、是否自启
journalctl -u 'v2bx@45678' -n 100 -f   # 单个实例的日志
v2bx log 45678                         # 同上（自动带 -f）
```
