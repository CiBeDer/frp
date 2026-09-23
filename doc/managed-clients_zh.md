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
