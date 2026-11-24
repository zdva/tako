# 文档

[简体中文首页](../README.md) · [English homepage](../README.en.md)

本页是仓库文档的地图。使用说明和贡献入口在上半部分；设计、计划和验收记录在下半部分，它们描述决定和当时的测量，不代替当前的使用说明。

一条决定被后来的 ADR 标成 *Superseded* 之后，只保留作历史。密钥存储的当前决定是 [ADR 0023](adr/0023-encrypted-secret-store.md)（Tako 自己的加密文件 `secrets.enc`）。[ADR 0003](adr/0003-secrets-in-keychain.md)（系统钥匙串）已作废。

## 使用

这些页面告诉你怎么操作。除本索引外，使用说明目前是英文。

| 文档 | 语言 | 内容 |
|---|---|---|
| [MANUAL.md](MANUAL.md) | English | 用户手册：主机、块、同步、监控、传输、设置 |
| [SHORTCUTS.md](SHORTCUTS.md) | English | 快捷键和鼠标手势 |
| [IMPORT.md](IMPORT.md) | English | 从 Xshell、SecureCRT、Termius、OpenSSH 导入，以及不含密码的导出 |
| [OSC7.md](OSC7.md) | English | 让 shell 报告当前目录，传输面板才知道从哪开 |

## 参与、安全、发布

| 文档 | 语言 | 内容 |
|---|---|---|
| [CONTRIBUTING.md](../CONTRIBUTING.md) | English | 如何提交修改 |
| [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) | English | 行为准则 |
| [SECURITY.md](../SECURITY.md) | English | 漏洞报告方式，以及密码文件的承诺和限度 |
| [CHANGELOG.md](../CHANGELOG.md) | English | 各版本对使用者可见的变化。验收数字不在这里 |
| [RELEASE.md](RELEASE.md) | English | 如何打发布包。目前没有 GitHub Release |
| [AGENTS.md](../AGENTS.md) | English | 给贡献者和编程代理的仓库约定。放在根目录，方便工具找到 |
| [THIRD_PARTY_LICENSES.md](../THIRD_PARTY_LICENSES.md) | English | 第三方许可证 |
| [LICENSE](../LICENSE) | English | GPL-3.0-or-later |

## 设计和开发档案

读这些文件时，以各文件开头的说明和 ADR 的 Status 为准。文中的周期、原型数据和「待确认」不是当前承诺。

| 文档 | 语言 | 内容 |
|---|---|---|
| [SPEC.md](SPEC.md) | English | 产品规格摘要，含里程碑范围 |
| [DESIGN.md](DESIGN.md) | 中文 | 交互和视觉规范。界面测试会对照其中的令牌和快捷键表 |
| [TECH-PLAN.md](TECH-PLAN.md) | 中文 | 2026-11 的技术方案和里程碑计划 |
| [TESTING.md](TESTING.md) | English | 测试分层和还没覆盖的部分 |
| [adr/](adr/README.md) | English | 架构决策记录 |
| [history/](history/README.md) | 中文 | 里程碑说明和当时的验收记录 |

`docs/design/mockups/` 是设计稿。`docs/screenshots/` 是 Linux（含 Xvfb）上的开发截图，画面里的主机名来自当时的测试环境，不是对外宣传图，也不表示 macOS 或 Windows 已经验收。
