# Tako（章鱼终端）

[English](README.md) · [简体中文](README.zh-CN.md)

Tako 是管理一群服务器的桌面终端。它把许多 SSH（和本地）终端放在同一屏的网格里（每个叫一个“块”），网络断了的块会自动重连，下次打开还记得你开过什么。用 Go 和 [MyGo](https://github.com/egoist/mygo) 原生 UI 工具包写成，支持 macOS、Windows、Linux。

> **状态：里程碑 M1（v0.1.0-m1），预发布。** 已有：连接、终端、自适应网格、主机侧栏、会话恢复、导入 `~/.ssh/config`。同步输入界面和小地图（M2）、监控和文件传输（M3）、打包打磨（M4）还没做。详见 [docs/SPEC.md](docs/SPEC.md)、[docs/TECH-PLAN.md](docs/TECH-PLAN.md)。

![主窗口](docs/screenshots/m1/main-light.png)

| 深色 | 连接中断 | 添加主机 |
|---|---|---|
| ![深色](docs/screenshots/m1/main-dark.png) | ![断线](docs/screenshots/m1/disconnected.png) | ![添加主机](docs/screenshots/m1/add-host.png) |

更多截图在 [docs/screenshots/m1](docs/screenshots/m1)。

## M1 能做什么

- **SSH**：密码、公钥（OpenSSH、PEM、PuTTY `.ppk`）、SSH Agent、键盘交互认证；跳板机链；SOCKS5 和 HTTP 代理；保活；服务器 MaxSessions 不够时自动多开一条连接。
- **不怕断线的面板**：链路断开时，块里显示重连横幅，滚动历史保留，按 1、2、5、10、30 秒退避重试，断线期间敲的键**不会**在恢复后补发。见 [ADR 0006](docs/adr/0006-session-pane-contract.md)。
- **主机密钥**：Tako 自己的 `known_hosts`；密钥变了**一律拒绝**并显示新旧指纹。见 [ADR 0004](docs/adr/0004-host-key-policy.md)。
- **秘密**只放系统钥匙串，不进文件和日志。见 [ADR 0003](docs/adr/0003-secrets-in-keychain.md)。
- **块网格**：1 / 2 / 2×2 / 3×2 / 超出后滚动；每块多标签页；本地终端；右键菜单；命令面板；中英文；浅色与深色。
- **导入** `~/.ssh/config`（支持 `Include`、`ProxyJump`、`IdentityFile`）。
- **会话恢复**：正常退出或崩溃（包括 `kill -9`）后，块和标签页会恢复并自动重连，通常不到一秒。

## 安装与运行

暂无发布的安装包，请从源码构建（Go 1.27.3 或更新）：

```sh
git clone <仓库地址> tako && cd tako
make run            # go run ./cmd/tako
make build          # ./tako（普通构建会在首次使用时下载 libghostty-vt）
make package-linux  # 打包的应用在 build/linux-amd64（自带 libghostty-vt）
make build-all      # Windows、macOS（通用）、Linux，amd64 与 arm64
```

打包版（`mygo build`）把终端引擎放在可执行文件旁边，离线也能用。普通 `go build` 的程序会在第一次打开终端时，从 MyGo 的发布页下载 `libghostty-vt` 到用户缓存目录，并校验 SHA-256。

二进制**还没有代码签名**：macOS 的 Gatekeeper 和 Windows 的 SmartScreen 会提示警告。

### 快捷键

Linux 和 Windows 的主修饰键是 `Ctrl+Shift`（这样单独的 `Ctrl+C` 还能到达 shell），macOS 用 `⌘`。

| 按键 | 作用 |
|---|---|
| `Ctrl+Shift+P` | 命令面板（主机、命令） |
| `Ctrl+Shift+N` | 添加主机 |
| `Ctrl+Shift+T` | 新标签页 / 本地终端 |
| `Ctrl+Shift+W` | 关闭标签页 · `Ctrl+Shift+Alt+W` 关闭块 |
| `Ctrl+Shift+1…9` | 切换标签页 |
| `Ctrl+Shift+Alt+←↑→↓` | 在块之间移动焦点 |
| `Ctrl+Shift+Alt+S` | 切换当前块的同步输入 |
| `Ctrl+Shift+B` | 显示 / 隐藏侧栏 |
| `Ctrl+Shift+,` | 设置 |

### 数据放在哪

| | Linux | macOS | Windows |
|---|---|---|---|
| 配置（`hosts.json`、`known_hosts`、`settings.json`） | `~/.config/tako` | `~/Library/Application Support/Tako` | `%AppData%\Tako` |
| 状态（`session.json`、日志、锁） | `~/.local/state/tako` | 同配置目录 | `%LocalAppData%\Tako` |

可用 `TAKO_CONFIG_DIR`、`TAKO_STATE_DIR` 改位置。没有系统凭据库的机器（比如没有 Secret Service 的无桌面 Linux），“记住密码”只在本次运行内有效，Tako 会明确告知。

## 开发

```sh
make check          # gofmt、vet、golangci-lint、go test -race、覆盖率门槛（核心 80%）
make integration    # 对真实 OpenSSH 测试（scripts/fetch-sshd.sh 会下载便携版）
make e2e            # Xvfb 下的真窗口：空闲 CPU/内存、冷启动、kill -9 恢复
make screenshots    # 真窗口截图，输出到 docs/screenshots/m1
make vuln           # govulncheck
```

代码结构与约定见 [AGENTS.md](AGENTS.md)，提交修改的方式见 [CONTRIBUTING.md](CONTRIBUTING.md)，测试环境见 [docs/TESTING.md](docs/TESTING.md)。

## 给维护者的说明

- Go 模块路径 `github.com/wei-dong/tako` 是**占位**，仓库确定后会用一个提交统一改掉。
- [MIT 许可证](LICENSE)是**占位，等所有者确认**。

## 许可证

MIT，见 [LICENSE](LICENSE)（待确认，见上）。
