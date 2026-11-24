# 里程碑

记录日期：2026-11。标签 `v0.1.0-m1`、`v0.2.0-m2`、`v0.3.0-m3`、`v0.4.0` 都在仓库里。没有对应的 GitHub Release。`main` 在 `v0.4.0` 之后还有未单独发布的改动，以 [CHANGELOG.md](../../CHANGELOG.md) 的 Unreleased 为准。

当时的验收表在 [acceptance.md](acceptance.md)。原首页上按 M1–M4 展开的长说明已收成本页；逐字原文在该次整理之前的 `README.md` / `README.zh-CN.md` git 历史里。

## M1 · v0.1.0-m1

连接、终端、自适应网格、主机侧栏、标签页、命令面板、从 `~/.ssh/config` 导入、断线后面板不结束、会话恢复、英文和简体中文、浅色和深色。

这一版把密码放在系统凭据库（[ADR 0003](../adr/0003-secrets-in-keychain.md)）。该决定已作废。

## M2 · v0.2.0-m2

同步输入的界面、悬浮小地图、可拖动并吸附的分割布局、快捷键、右键菜单、块重命名、自定义标题栏。布局和同步状态可以随会话恢复。

## M3 · v0.3.0-m3

不在服务器上安装程序的监控，以及上传、下载和服务器之间的文件传输（经本机中转，或可选的 `scp` / `rsync` 直连），含队列和续传。传输面板的默认目录来自 OSC 7。

Linux 采集器在真机 OpenSSH 上对过读数。macOS、Windows 和 GPU 的解析器用的是按工具文档手写的样例。

## M4 · v0.4.0

设置窗口、终端配色（含 iTerm2 `.itermcolors`）、英文和简体中文的伪语言检查、不含秘密的导出，以及 Xshell / SecureCRT / Termius 导入。导入器按公开文档编写，样例不是这些软件的真实导出。另有无障碍和输入法相关测试、崩溃报告、打包和 ed25519 签名的更新流程。安装包不做 Apple / Windows 代码签名。编码确定只支持 UTF-8。

发布清单里需要人在真机上做的项目，当时没有做完。清单仍是 [RELEASE.md](../RELEASE.md)。

## 后来取代的说法

- 密码的当前位置是 Tako 自己的 `secrets.enc`，见 [ADR 0023](../adr/0023-encrypted-secret-store.md)。不要按 ADR 0003 或 M1 的 changelog 条目去找系统钥匙串。
- 本地终端现在由 Tako 自己的伪终端运行，并参与同步输入。M1 记录里「本地终端用 MyGo 内置 PTY」描述的是当时的实现。
