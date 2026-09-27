# 原生方式启动 frps

以下命令在仓库根目录执行，适用于 `dev-multi` 分支。frps 直接运行本机二进制，不依赖 Docker；客户端仍使用官方 frpc。

## 构建

需要 Go 1.25、Node.js 和 npm。先构建 Dashboard，再编译 frps，以便把当前页面嵌入二进制：

```powershell
cd web
npm ci
npm run build --workspace frps
cd ..
go build -tags frps -o bin/frps.exe ./cmd/frps
```

Linux/macOS 把输出文件改为 `bin/frps`。如果已经有按本分支源码构建的二进制，可以跳过构建。

## 准备配置

以 PowerShell 为例，把示例复制到 Git 忽略的本地目录：

```powershell
New-Item -ItemType Directory -Force .cache/frps-native | Out-Null
Copy-Item conf/frps_managed_example.toml .cache/frps-native/frps.toml
```

编辑 `.cache/frps-native/frps.toml`：

- 将 `webServer.password` 换成自己的管理密码，不能继续用示例密码。
- 将 `clientManagement.storePath` 设为 `"./.cache/frps-native/clients.json"`，以便客户端档案保存在独立目录。这个文件包含客户端原始密钥，应限制访问并备份。
- 本机预览可设置 `bindAddr = "127.0.0.1"`，Dashboard 保持 `webServer.addr = "127.0.0.1"`。如果要让其他机器的 frpc 连接，按实际网络环境将 `bindAddr` 设为服务器可访问的地址，并放行连接端口。
- 如需限制代理端口，可添加 `allowPorts = [{ start = 20000, end = 20099 }]`；这段范围内的具体端口仍需在 **Clients** 页面授予对应客户端。

相对路径以启动 frps 时的工作目录为准，因此以下命令都从仓库根目录运行。

## 启动与访问

```powershell
.\bin\frps.exe verify -c .\.cache\frps-native\frps.toml
.\bin\frps.exe -c .\.cache\frps-native\frps.toml
```

Linux/macOS 将二进制路径改为 `./bin/frps`。第二条命令保持前台运行；看到 `frps started successfully` 后，打开 `http://127.0.0.1:7500/static/#/clients`，输入 `webServer.user` 和 `webServer.password`。`7000/tcp` 是默认 frpc 连接端口，`7500/tcp` 是 Dashboard 端口。前台运行时按 `Ctrl+C` 停止服务。

在 **Clients** 页面新增客户端，配置密钥和一条或多条 TCP/UDP 代理，保存后点击 **Copy TOML**。把复制的内容保存为客户端的 `frpc.toml`，在客户端执行 `frpc -c frpc.toml`。如果 frpc 不在服务器本机，新增客户端时填写它能访问的服务器地址；本机预览配置的 `127.0.0.1` 仅供本机连接。

按上述步骤启动时，本地配置和客户端档案分别位于 `.cache/frps-native/frps.toml` 与 `.cache/frps-native/clients.json`，与 Docker Compose 的数据卷分开。该目录已被 Git 忽略；不要提交本机管理密码或客户端档案。
