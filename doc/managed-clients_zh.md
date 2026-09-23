# Dashboard 客户端与端口授权

本分支在 frps 上增加客户端档案管理，使用官方 frpc 的标准 token 鉴权和 TOML 配置，无需修改或重新编译 frpc。

## 开启

在 frps.toml 顶层添加以下字段（放在任何 `[section]` / `[[array]]` 之前）：

```toml
auth.method = "token"
clientManagement.enabled = true
clientManagement.storePath = "./frps-clients.json"

webServer.addr = "127.0.0.1"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "替换为你的管理密码"
```

重启 frps，打开 Dashboard 的 **Clients** 页面，点击 **Add Client**。填写名称、客户端密钥（留空自动生成）、frpc 可访问的服务器域名或 IP，然后添加一条或多条代理：名称、TCP/UDP、本地地址、本地端口、服务端远程端口。

保存后立即生效，客户端未连接时也会显示在列表中。点击 **Copy TOML**，把完整配置保存为 `frpc.toml`，使用官方客户端运行：

```sh
frpc -c frpc.toml
```

复制的 `serverPort` 来自 frps 的 `bindPort`；如果部署经过外部端口映射，需要在 frpc.toml 中改成实际入口端口。`serverAddr` 应填公网地址或客户端可以访问的内网地址，不要填监听地址 `0.0.0.0`。

## 流量查看

客户端的 Proxies 列表和代理详情会显示今日总流量（入站 + 出站）。详情的流量统计读取 frps 现有的最近 7 天每日汇总，可以选择该范围内的起止日期；点击 Query 后，显示包含起止日期的区间总量、入站和出站流量，每个日期列下方也会显示当天总量。

日期按服务端的每日统计口径显示，只支持按天筛选。筛选和求和在浏览器内完成，不新增采集、数据库或统计写入。保持原有规则：统计只存在于内存，frps 重启后清空，TCP 流量在连接结束时计入。

## 授权规则

- 未开启时保持官方原有鉴权行为；开启后仅接受已登记的客户端 token，原来的全局 `auth.token` 不作为后备凭据。开启前请把现有客户端登记到列表并更新其配置。
- 客户端身份由验证成功的密钥确定，不能通过伪造 `user`、`clientID` 或其他客户端的 `runID` 获得其他客户端权限。
- 允许使用的远程端口从该客户端的代理模板推导，按 TCP/UDP 分别授权；未授权端口、随机端口 `0`、其他代理类型和端口负载均衡组会被拒绝。
- 模板中的本地地址和本地端口用于生成配置；frps 不能验证客户端实际转发到哪个本地服务。服务端强制执行的是远程端口与协议权限。
- 全局 `allowPorts`、`maxPortsPerClient` 仍生效；重复密钥、同协议的重复端口、无效配置会被拒绝。
- 一个客户端档案对应一个在线客户端实例；重连沿用原有 frp 会话机制。多台客户端请创建多个档案。
- 当前支持新增和复制配置；暂不提供编辑、删除、密钥轮换或在线配置推送。复制后仍需把 TOML 配置放到客户端运行。
- 受管模式不兼容 OIDC 或 SSH Tunnel Gateway，启用时会校验并拒绝这些组合。

## 存储与构建

档案存储在 `clientManagement.storePath`，相对路径以 frps 进程工作目录为准。写入通过临时文件替换完成；读取损坏的配置会停止启动，避免放宽鉴权。文件包含生成客户端配置所需的原始密钥，请仅允许服务端运行账户和管理员访问，并将它作为持久数据备份；容器部署需要挂载对应目录。一个文件只供一个 frps 进程使用。

前端需重新构建后编译 frps，才能把新的 Clients 页面嵌入二进制：

```sh
cd web
npm ci
npm run build --workspace frps
cd ..
go build -tags frps -o bin/frps ./cmd/frps
```

可运行示例见 `conf/frps_managed_example.toml`。该示例默认只在本机开放 Dashboard，远程管理可通过 SSH 转发或受保护的 HTTPS 反向代理访问。

直接运行本机二进制的构建、配置和启动步骤见 [原生方式启动 frps](native-start_zh.md)。

## Docker Compose 部署

仓库根目录的 `compose.yaml` 从当前分支源码构建 frps，并嵌入新 Dashboard。首次启动前复制配置模板：

```sh
cp deploy/frps.toml.example deploy/frps.toml
```

Windows PowerShell 可用 `Copy-Item deploy/frps.toml.example deploy/frps.toml`。编辑 `deploy/frps.toml`，把 `webServer.password` 换成自己的强密码。此文件由 Compose 作为运行时 secret 挂载，不会放进镜像，也已被 Git 和 Docker 构建上下文排除。Linux 主机上请限制 `deploy` 目录只允许管理员访问，并确保容器内 UID 10001 能读取该配置文件。

```sh
docker compose config -q
docker compose up -d --build
docker compose ps
docker compose logs -f frps
```

`7000/tcp` 为 frpc 连接端口；`20000–20099` 的 TCP 和 UDP 是可分配的代理端口。Dashboard 只在服务器本机 `127.0.0.1:7500` 可访问，远程管理可用 `ssh -L 7500:127.0.0.1:7500 user@server`，然后打开 `http://127.0.0.1:7500/static/`。管理客户端时，`serverAddr` 填服务器的公网域名或 IP；生成的 `serverPort` 为 7000。

Compose 默认限制为 2 CPU、1 GiB 内存；连接数较多时可按服务器资源调整 `compose.yaml`。

如果需要其他代理端口，请同时修改 `compose.yaml` 中的 TCP/UDP 映射和 `deploy/frps.toml` 的 `allowPorts`，再重建容器。容器内端口与宿主机端口应保持一致，生成的 frpc TOML 才能直接使用。当前受管客户端只支持 TCP/UDP；HTTP/HTTPS 类型需要另行扩展授权与容器端口配置。

客户端档案保存在 Compose 命名卷 `frps_data` 的 `/var/lib/frp/clients.json` 中。首次新增客户端后，升级前可用以下命令备份档案并保留旧镜像：

```sh
docker compose cp frps:/var/lib/frp/clients.json deploy/clients.backup.json
docker image tag frps-dev-multi:local frps-dev-multi:rollback
docker compose up -d --build
```

同时备份 `deploy/frps.toml`。回滚时把旧镜像重新标记并启动，保持同一个档案卷：

```sh
docker image tag frps-dev-multi:rollback frps-dev-multi:local
docker compose up -d --no-build --force-recreate frps
docker compose ps
```

不要执行 `docker compose down -v`，它会删除档案卷。档案和备份都含原始客户端密钥，应限制访问。若新版本更改了档案格式，回滚前还需恢复对应版本的档案备份。
