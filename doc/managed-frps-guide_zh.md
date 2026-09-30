# CiBeDer/frp 魔改版使用手册

> 适用分支：`dev-multi`  
> 服务端镜像：`ghcr.io/cibeder/frps:dev-multi-latest`  
> 上游项目：`fatedier/frp`

本分支基于官方 frp 增加了 **frps 受管客户端（Managed Clients）管理能力**，主要用于在服务端 Web Dashboard 中集中创建客户端、分配端口、生成 `frpc.toml`、启停客户端/代理、查看端口在线状态、重置客户端密钥以及备份/恢复客户端数据。

## 1. 本分支增加了什么

在官方 frps 功能基础上，`dev-multi` 主要增加：

- Web Dashboard 中创建、编辑、删除受管客户端。
- 每个客户端独立随机密钥，不需要所有 frpc 共用同一个 token。
- 在 Dashboard 中直接生成、复制、下载客户端 `frpc.toml`。
- 客户端级启用/停用。
- 单个代理级启用/停用。
- TCP / UDP 远程端口池管理。
- 新增或编辑代理时自动校验端口范围与占用情况。
- 删除代理或客户端后自动释放端口。
- 停用/离线状态不会释放端口，防止被其他客户端抢占。
- 客户端列表直接显示已绑定端口。
  - 绿色：代理当前已在 frps 注册并在线。
  - 红色：端口仍属于该客户端，但代理当前不在线。
- Recent events：显示连接、鉴权失败、代理拒绝、启停、编辑、删除、密钥重置等近期事件。
- 客户端密钥 Reset，旧密钥立即失效。
- JSON Backup / Restore。
- Docker Compose 的端口发布范围与 frps `allowPorts` 使用同一组环境变量。
- GitHub Actions 自动发布 amd64 / arm64 多架构 GHCR 镜像。

---

## 2. 端口说明

默认配置：

| 端口 | 协议 | 用途 | 是否需要公网开放 |
| --- | --- | --- | --- |
| `7000` | TCP | frpc 连接 frps 的控制端口 | 是 |
| `7500` | TCP | frps Web Dashboard | 默认仅绑定服务器 `127.0.0.1` |
| `20000-20099` | TCP + UDP | 分配给客户端代理的远程端口池 | 按实际使用开放 |

默认 `compose.yaml` 将 Dashboard 映射为：

```yaml
127.0.0.1:7500:7500/tcp
```

因此公网直接访问 `http://服务器IP:7500` **默认不会成功，这是有意的安全设计**。

推荐通过 SSH 转发访问：

```bash
ssh -L 7500:127.0.0.1:7500 用户名@服务器IP
```

然后浏览器打开：

```text
http://127.0.0.1:7500
```

如果确实要把 Dashboard 暴露到公网，需要自行修改 `compose.yaml`，同时建议至少配合防火墙白名单或 HTTPS 反向代理，不建议直接裸露管理端口。

---

## 3. 推荐部署方式：Docker Compose 从源码构建

### 3.1 拉取仓库

```bash
git clone https://github.com/CiBeDer/frp.git
cd frp
git switch dev-multi
```

如果已经克隆过：

```bash
git fetch origin
git switch dev-multi
git pull --ff-only origin dev-multi
```

### 3.2 创建环境变量文件

```bash
cp .env.example .env
```

默认：

```env
FRPS_PROXY_PORT_START=20000
FRPS_PROXY_PORT_END=20099
```

这两个值是代理端口池的唯一 Compose 输入，同时用于：

1. Docker 发布到宿主机的 TCP / UDP 端口范围。
2. 注入容器的环境变量。
3. `frps.toml` 中的 `allowPorts`。

因此需要修改代理端口池时，只改 `.env` 即可，例如：

```env
FRPS_PROXY_PORT_START=21000
FRPS_PROXY_PORT_END=21999
```

修改后重新创建容器：

```bash
docker compose up -d --build
```

云服务器安全组/防火墙也必须开放相应端口，否则 frps 内部显示正常但公网仍无法连接。

### 3.3 创建 frps 配置

```bash
cp deploy/frps.toml.example deploy/frps.toml
```

至少修改 Dashboard 密码：

```toml
webServer.user = "admin"
webServer.password = "换成强密码"
```

默认关键配置：

```toml
bindAddr = "0.0.0.0"
bindPort = 7000

auth.method = "token"

clientManagement.enabled = true
clientManagement.storePath = "/var/lib/frp/clients.json"

webServer.addr = "0.0.0.0"
webServer.port = 7500

allowPorts = [
  { start = {{ .Envs.FRPS_PROXY_PORT_START }}, end = {{ .Envs.FRPS_PROXY_PORT_END }} }
]
```

注意：容器内部 Dashboard 必须监听 `0.0.0.0`，真正限制外部访问的是 Docker Compose 的 `127.0.0.1:7500:7500` 映射。

### 3.4 启动

```bash
docker compose up -d --build
```

查看状态：

```bash
docker compose ps
```

查看日志：

```bash
docker compose logs -f frps
```

健康检查：

```bash
curl http://127.0.0.1:7500/healthz
```

---

## 4. 使用 GHCR 预构建镜像

`dev-multi` 每次 push 后会自动构建多架构镜像：

```text
ghcr.io/cibeder/frps:dev-multi-latest
```

支持：

- `linux/amd64`
- `linux/arm64`

拉取：

```bash
docker pull ghcr.io/cibeder/frps:dev-multi-latest
```

如果 GHCR Package 被设置为私有，需要先：

```bash
echo "YOUR_GITHUB_PAT" | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

直接使用镜像运行的示例：

```bash
docker volume create frps_data

docker run -d \
  --name frps \
  --restart unless-stopped \
  -p 7000:7000/tcp \
  -p 127.0.0.1:7500:7500/tcp \
  -p 20000-20099:20000-20099/tcp \
  -p 20000-20099:20000-20099/udp \
  -e FRPS_PROXY_PORT_START=20000 \
  -e FRPS_PROXY_PORT_END=20099 \
  -v "$PWD/deploy/frps.toml:/etc/frp/frps.toml:ro" \
  -v frps_data:/var/lib/frp \
  ghcr.io/cibeder/frps:dev-multi-latest \
  -c /etc/frp/frps.toml
```

镜像中的 frps 使用 UID/GID `10001` 运行。使用 bind mount 挂载配置时，请确保容器用户能够读取该文件。

---

## 5. 第一次进入 Dashboard

通过 SSH 转发访问：

```bash
ssh -L 7500:127.0.0.1:7500 用户名@服务器IP
```

浏览器：

```text
http://127.0.0.1:7500
```

使用 `deploy/frps.toml` 中配置的：

- `webServer.user`
- `webServer.password`

登录。

---

## 6. 创建客户端

进入 Dashboard 的 **Managed Clients** 区域，创建新客户端。

### Client Name

客户端备注名，例如：

```text
fnos-home
```

### Server Address

填写 **客户端实际能够访问到的 frps 地址**，例如：

```text
frp.example.com
```

或：

```text
1.2.3.4
```

不要填写：

```text
http://1.2.3.4
0.0.0.0
```

`serverAddr` 是给 frpc 使用的连接目标，不是 frps 的监听地址。

如果服务器经过 NAT、端口映射、负载均衡或其他入口，下载配置后还要确认 `serverPort` 是客户端实际能访问到的入口端口。

### Proxy

每个 Proxy 主要填写：

- Name：代理名称。
- Type：`tcp` 或 `udp`。
- Local IP：客户端本机需要转发的服务地址，常用 `127.0.0.1`。
- Local Port：客户端本地服务端口。
- Remote Port：frps 公网对外端口。

例如把家里 NAS 的 SSH `22` 端口映射到公网 `20022`：

```text
Name: nas-ssh
Type: tcp
Local IP: 127.0.0.1
Local Port: 22
Remote Port: 20022
```

Remote Port 必须：

- 位于服务器允许的端口池中；
- 没有被 frps 自身监听端口占用；
- 没有被其他客户端占用；
- 同一客户端内不能重复分配。

Dashboard 前端会校验一次，服务端还会再次校验，因此不能通过绕过前端强行抢占端口。

---

## 7. 下载并运行 frpc

创建客户端后，可以直接：

- View TOML
- Copy TOML
- Download

生成的 `frpc.toml` 已包含该客户端专属密钥和当前启用的代理。

推荐客户端使用与服务端兼容的官方 frpc。服务端的客户端管理功能仍基于官方 frpc 协议工作，不要求使用魔改版 frpc。

Linux 示例：

```bash
./frpc -c ./frpc.toml
```

检查配置：

```bash
./frpc verify -c ./frpc.toml
```

如果希望长期运行，可配置 systemd：

```ini
[Unit]
Description=frpc
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/frp/frpc -c /opt/frp/frpc.toml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

然后：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now frpc
sudo systemctl status frpc
```

---

## 8. 客户端与代理状态

### 客户端开关

停用 Client：

- frps 会断开该客户端当前控制连接；
- 该客户端密钥暂时不能重新登录；
- 已分配端口继续保留，不会释放。

重新启用后，frpc 可以重新连接。

### Proxy 开关

官方 frpc 没有服务端实时下发“只关闭一个代理”的控制消息。

因此服务端切换某个 Proxy 后，会主动回收该客户端控制连接，让官方 frpc 自动重连并重新注册代理：

- Disabled Proxy 在重新注册时会被拒绝。
- Enabled Proxy 会在重连后恢复。
- Disabled Proxy 的 Remote Port 仍然保留。

### 端口颜色

Managed Clients 首页：

- **绿色**：该代理当前已注册到 frps。
- **红色**：端口已分配，但代理当前离线。

红色不代表端口已经释放。

---

## 9. Port Pool

Dashboard 的 **Port Pool** 可以查看：

- 当前允许分配的端口范围；
- TCP / UDP 已分配端口；
- 所属 Client；
- 所属 Proxy；
- Client / Proxy 是否启用；
- 代理当前是否在线；
- frps 自身保留端口；
- 推荐的空闲 TCP / UDP 端口。

端口生命周期：

| 操作 | 是否释放 Remote Port |
| --- | --- |
| frpc 临时掉线 | 否 |
| 停用 Client | 否 |
| 停用 Proxy | 否 |
| 删除 Proxy | 是 |
| 删除 Client | 是 |

---

## 10. Reset Key

点击客户端的 **Reset key** 后：

1. 服务端生成新的随机客户端密钥。
2. 旧密钥立即失效。
3. 当前客户端连接会被断开。
4. Dashboard 会生成新的 `frpc.toml`。
5. 必须把新配置重新部署到客户端。

如果客户端不断出现鉴权失败，确认没有旧的 frpc 实例仍在使用旧配置反复重连。

---

## 11. Backup / Restore

### Dashboard Backup

点击 **Backup** 会下载 JSON 备份，其中包含：

- Client ID
- Client Name
- Client Key / Token
- Server Address
- Proxy 配置
- Remote Port 分配
- Enabled 状态

**这个文件包含客户端密钥，等同于敏感凭据，不要公开上传或提交到 Git。**

### Restore

Restore 采用整体替换语义：

- 当前 Managed Clients 数据会被备份内容替换；
- 当前受管客户端连接会断开；
- 恢复后的客户端按照备份中的密钥和授权重新连接。

### 服务端数据文件

默认数据：

```text
/var/lib/frp/clients.json
```

Compose 使用命名卷：

```text
frps_data
```

因此正常重建容器不会丢失 Managed Clients 数据。

额外备份示例：

```bash
docker cp frps:/var/lib/frp/clients.json ./clients.json.backup
```

同时建议单独备份：

```text
deploy/frps.toml
.env
```

---

## 12. 升级

### 源码 Compose 部署

```bash
git fetch origin
git switch dev-multi
git pull --ff-only origin dev-multi
docker compose up -d --build
```

然后检查：

```bash
docker compose ps
docker compose logs --tail=200 frps
```

Managed Clients 数据保存在 `frps_data` 卷中，重新构建镜像不会自动删除该卷。

不要执行：

```bash
docker compose down -v
```

除非明确希望删除数据卷。

### GHCR 镜像部署

```bash
docker pull ghcr.io/cibeder/frps:dev-multi-latest
```

然后按原参数重新创建容器。

生产环境如果希望版本可追溯，也可以使用提交 SHA 标签，而不是长期跟随 `dev-multi-latest`。

---

## 13. 防火墙 / 云安全组

至少需要：

### frpc 控制连接

```text
TCP 7000
```

### 实际对外代理端口

例如端口池是：

```text
20000-20099
```

则根据实际代理类型开放：

```text
TCP 20000-20099
UDP 20000-20099
```

Dashboard `7500` 默认不需要在云安全组中开放，因为 Compose 只绑定服务器本机 `127.0.0.1`。

---

## 14. 常见问题

### Dashboard 打不开

先在服务器执行：

```bash
curl http://127.0.0.1:7500/healthz
docker compose ps
docker compose logs --tail=200 frps
```

如果本机能通而公网 `IP:7500` 不通，这是默认行为。请使用 SSH 端口转发。

### frpc 连不上 frps

检查：

1. `serverAddr` 是否是客户端可访问的公网 IP / 域名。
2. `serverPort` 是否正确。
3. 云安全组是否开放 TCP 7000。
4. frps 是否真的在运行。
5. 客户端是否被 Disable。
6. 客户端是否仍在使用 Reset Key 之前的旧配置。

### 代理显示红色

红色表示端口仍已分配，但代理没有在 frps 当前运行时注册。

检查客户端：

```bash
./frpc verify -c ./frpc.toml
./frpc -c ./frpc.toml
```

以及服务端 Recent Events / logs。

### 外网访问 Remote Port 不通

如果 Dashboard 中已经显示绿色，重点检查：

- 云服务器安全组；
- 宿主机防火墙；
- Docker 是否发布了该端口；
- 该 Remote Port 是否位于 `.env` 配置的范围；
- 客户端 Local IP / Local Port 是否确实有服务监听。

查看 Docker 端口：

```bash
docker compose config
docker compose ps
```

### 提示 Remote Port 不允许

Remote Port 必须位于：

```env
FRPS_PROXY_PORT_START
FRPS_PROXY_PORT_END
```

定义的范围中。

修改 `.env` 后必须重新创建容器：

```bash
docker compose up -d --build
```

### 删除客户端后端口什么时候可以复用

删除 Client / Proxy 时，服务端会等待运行时代理清理完成后再释放端口，避免旧 listener 尚未关闭时端口就被重新分配。

---

## 15. 安全建议

- 修改默认 Dashboard 密码。
- 不要直接把 `7500` 暴露到公网。
- 建议通过 SSH Tunnel、VPN 或可信反向代理访问 Dashboard。
- Backup JSON 包含客户端密钥，按密码文件处理。
- `deploy/frps.toml` 可能包含管理密码，不要提交到 Git。
- 客户端 `frpc.toml` 包含专属密钥，不要公开。
- 只开放真正需要的代理端口。
- 定期备份 Managed Clients。
- Reset Key 后尽快更新客户端配置，停止旧 frpc 实例。

---

## 16. GitHub Actions / GHCR

工作流：

```text
.github/workflows/publish-frps-ghcr.yml
```

触发条件：

```text
push -> dev-multi
```

构建：

- amd64
- arm64

发布：

```text
ghcr.io/cibeder/frps:<commit-sha>
ghcr.io/cibeder/frps:dev-multi-latest
```

如果需要固定部署版本，推荐记录具体 commit SHA 镜像标签。

---

## 17. 与官方 frp 的关系

该仓库是官方 `fatedier/frp` 的 Fork。

官方文档仍适用于标准 frp 功能，例如：

- TCP / UDP / HTTP / HTTPS 代理；
- KCP / QUIC；
- TLS；
- HTTP 路由；
- 插件；
- 健康检查；
- 负载均衡；
- P2P 等。

本手册重点说明 `CiBeDer/frp:dev-multi` 自定义增加的 **Managed Clients + Docker/GHCR 部署能力**。

官方文档：

- https://gofrp.org
- https://github.com/fatedier/frp

