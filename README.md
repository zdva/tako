# Tako（章鱼终端）

[简体中文](README.md) · [English](README.en.md)

[![License: GPL-3.0-or-later](https://img.shields.io/badge/license-GPL--3.0--or--later-blue.svg)](LICENSE)
[![CI](https://github.com/zdva/tako/actions/workflows/ci.yml/badge.svg)](https://github.com/zdva/tako/actions/workflows/ci.yml)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8.svg)](go.mod)

Tako 是给一群服务器用的桌面 SSH 终端。同一窗口里并排打开多台机器（每一格叫一个「块」），断线后保留画面并重连，退出后再打开会回到上次的块和标签页。界面用 [MyGo](https://github.com/egoist/mygo)，语言有简体中文和 English。

## 能做什么

- **多台机器同屏**。块里可以再开标签页，也可以开本地终端。拖分隔线调整比例，会吸附到 ½、⅓、⅔。
- **同步输入**。若干块打开同步后，在一块里打的字会发到其他同步块。掉线的块会暂停，断线期间的输入不会在恢复后补发。
- **小地图**。看哪些块在同步，可以点选、框选，也可以拖动重排。
- **SSH**。密码、公钥（OpenSSH、PEM、PuTTY `.ppk`）、SSH Agent、键盘交互；跳板链；SOCKS5 与 HTTP 代理。
- **主机密钥**。使用 Tako 自己的 `known_hosts`。密钥变了就拒绝连接，并显示新旧指纹。
- **监控**。不在服务器上安装程序。块底部显示 CPU、内存、磁盘；主机上有 `nvidia-smi` 或 `rocm-smi` 时也显示 GPU。点底部进入监控页。
- **传文件**。上传、下载，以及两台服务器之间互传（经本机中转，或在设置里允许后用 `scp` / `rsync` 直连）。有队列，可以暂停和续传。
- **主机库**。从 `~/.ssh/config`、Xshell、SecureCRT、Termius 导入。导出文件不含密码。
- **设置立即生效**。语言、浅色/深色、终端配色（含导入 iTerm2 `.itermcolors`）、字体、保活、监控和传输。

编码只有 UTF-8。

## 现状

源码里的版本号是 **0.4.0**。仓库有标签 [`v0.4.0`](https://github.com/zdva/tako/tree/v0.4.0)，**没有 GitHub Release，也没有可下载的安装包**。请从下面的源码构建。

`main` 比标签 `v0.4.0` 新。已经合入、尚未单独发布的变化写在 [CHANGELOG.md](CHANGELOG.md) 的 Unreleased，其中包括密码改存 Tako 自己的加密文件。

构建目标包含 macOS、Windows 和 Linux。打包配置里的 macOS 最低系统版本是 13.0。发布验收记录来自 Linux；macOS 与 Windows 还没有在真机上安装并完成验收的记录。持续集成的结果以 [Actions](https://github.com/zdva/tako/actions/workflows/ci.yml) 为准。

## 从源码运行

需要 Go 1.27.3 或更新（`go.mod` 的 toolchain 是 `go1.27.4`），以及能开窗口的图形环境。

```sh
git clone https://github.com/zdva/tako.git
cd tako
make run
```

`make build` 生成当前平台的 `./tako`。这种普通构建在第一次打开终端时，会从 MyGo 的发布页下载 `libghostty-vt` 到用户缓存，并校验 SHA-256。

`make package-linux` 打出 `build/linux-amd64`，把 `libghostty-vt` 放在可执行文件旁边，之后可以离线开终端。`make build-all` 交叉编译 Windows、macOS（universal）和 Linux 的二进制，不打安装包、不签名。

打上版本标签后，发布工作流会生成安装包草稿（tar.gz、deb、rpm、PKGBUILD、dmg、NSIS），需要人核对再公开。二进制不做 Apple / Windows 代码签名，Gatekeeper 和 SmartScreen 会提示。打开方式见 [docs/MANUAL.md](docs/MANUAL.md)，以后如何加签名见 [docs/RELEASE.md](docs/RELEASE.md)。

Linux 上开窗口需要图形环境。本仓库的 RPM 配方声明依赖 `gtk3`。deb 的系统依赖不在本仓库里列出。

## 开始用

1. 第一次打开可以选择添加主机、本地终端，以及（本机有 `~/.ssh/config` 时）导入。
2. 第一次连接时确认主机指纹。以后这台机器的密钥变了，Tako 会拒绝。
3. 密码、私钥口令和代理密码写在配置目录的 `secrets.enc`（权限 0600）。Tako 不使用系统钥匙串，文件里也没有主密码。
4. 打开设置：Linux / Windows 为 `Ctrl+Shift+,`，macOS 为 `⌘,`。

Linux / Windows 的主键是 `Ctrl+Shift`，单独的 `Ctrl+C` 仍交给 shell。macOS 用 `⌘`。快捷键表是 [docs/SHORTCUTS.md](docs/SHORTCUTS.md)。操作说明是 [docs/MANUAL.md](docs/MANUAL.md)（英文）。文档地图是 [docs/README.md](docs/README.md)。

### 数据目录

| | Linux | macOS | Windows |
|---|---|---|---|
| 配置（`hosts.json`、`secrets.enc`、`known_hosts`、`settings.json`） | `~/.config/tako` | `~/Library/Application Support/Tako` | `%AppData%\Tako` |
| 状态（`session.json`、日志） | `~/.local/state/tako` | 与配置同一目录 | `%LocalAppData%\Tako` |

可用 `TAKO_CONFIG_DIR` 和 `TAKO_STATE_DIR` 改位置。

加密密钥绑在这台机器上。读不到机器标识时，密钥改放旁边的 `secrets.key`。只拷走 `secrets.enc`、没有这台机器的标识或 `secrets.key`，在另一台电脑上解不开。能挡住什么、挡不住什么，写在 [SECURITY.md](SECURITY.md)。

更早的构建曾把密码放在系统凭据库里，服务名是 `dev.tako.app`。那条决定（[ADR 0003](docs/adr/0003-secrets-in-keychain.md)）已经作废，现在的启动过程不读那些条目。下次连接时重新输入并选择记住，或退出后运行一次 `tako --migrate-keychain`（系统可能会按条目询问）。当前决定是 [ADR 0023](docs/adr/0023-encrypted-secret-store.md)。

## 使用前需要知道的限制

- 监控在 Linux 上对过本机 OpenSSH。macOS、Windows 和 GPU 的解析器是按工具文档手写的样例，不是实机录制。
- Xshell、SecureCRT、Termius 的导入按公开格式编写。仓库里的样例不是这些软件导出的真实文件。
- 两台服务器直连拷贝目录时，不能从中途续传。直连需要本机 SSH Agent 里已经有源主机的密钥。
- 崩溃报告和「复制诊断信息」不会上传。贴到 issue 之前请再看一眼内容。

## 参与和安全

提交修改见 [CONTRIBUTING.md](CONTRIBUTING.md)。仓库布局见 [AGENTS.md](AGENTS.md)，测试分层见 [docs/TESTING.md](docs/TESTING.md)。行为准则是 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。漏洞不要开公开 issue，见 [SECURITY.md](SECURITY.md)。

## 许可证

Tako 以 [GNU 通用公共许可证](LICENSE)第 3 版或（由你选择）任何更新版本分发（**GPL-3.0-or-later**），不提供任何担保。第三方组件各自的许可证见 [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)。
