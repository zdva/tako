# Tako（章鱼终端）技术方案与里程碑

> 流程第 4 步。依据：产品规格 Page、`DESIGN.md`（定稿）、技术验证原型 `/workspace/tako-spike`（约 3700 行 Go，含测试与实测数据）。
> 只用 **MyGo**（v0.8.3，Go 1.27.3）。Go 代码规范：samber 的 `golang-*` 技能（尤其 project-layout、design-patterns、error-handling、concurrency、context、security、testing、lint）和 JetBrains 的 `use-modern-go`。
> 带 **【待确认】** 的地方，是我替你做了默认决定、但需要你点头的事，汇总在 §14。

> **定稿后的用户决定（2026-11-11，覆盖本文）**：① 不做 GBK / 非 UTF-8 编码，只支持 UTF-8（见 ADR 0005）；② 暂不做代码签名、Apple 公证、Windows 证书，发布未签名安装包，自动更新使用 MyGo 的 ed25519 签名更新（密钥只放 CI Secrets，不需要任何平台证书）；③ 不做“同步输入时 sudo 密码保护”。本文里与此冲突的条目（§5 编码、§6 签名公证、M4 范围、§14 第 3/5/7 项）以此为准。

---

## 0. 一页纸摘要

- **结构**：`cmd/tako` 只做装配；核心逻辑放 `internal/` 下按职责拆的小包，**核心包不依赖 MyGo UI**，只有 `internal/ui`、`internal/secrets`（钥匙串）、`internal/platform` 碰 MyGo。UI 与核心之间用小接口隔开，核心可以在没有窗口的情况下做完整测试。
- **关键约定**：终端和 SSH 之间永远隔着一个 `session.Pane`（实现 `io.ReadWriteCloser` + `Resize`）。底层 SSH 重建时 `Pane` 不变，`Read` 阻塞而不返回 EOF（原型已验证）。同步输入走 `InputBus`，每个目标一条队列，不回环（原型已验证）。
- **存储**：JSON 文件 + 原子写入 + `version` 字段做迁移；密码、口令、私钥口令只放系统钥匙串，配置文件和导出文件里只有“钥匙串引用”。
- **布局**：自动模式 = 按窗口宽度算的网格；手动模式 = 二叉分割树（支持分隔线比例、边缘插入线、吸附）。小地图画的就是同一份矩形。
- **里程碑**：M1 连接 + 终端 + 网格 + 侧栏（约 3 周）→ M2 同步 + 小地图 + 吸附（约 2.5 周）→ M3 监控 + 传输（约 3.5 周）→ M4 打磨 / 国际化 / 深色 / 打包发布（约 3 周）。合计约 12 周（单人，AI 辅助开发，含测试；不含 Apple / Windows 证书申请等外部等待）。
- **最大风险**：MyGo 还在 0.x（API 可能变）；Windows / macOS 构建产物从没在真机上跑过；GPU 渲染性能没测；真实 OpenSSH 的兼容性（跳板机、agent、键盘交互）只靠 M1 的 Docker 集成测试来补。

---

## 1. 前提：原型已经证明了什么、没证明什么

| 项 | 结论 | 本方案怎么用 |
|---|---|---|
| 9 个终端同屏 | 空闲 CPU 0%、RSS 61–66 MB；9 块同时狂刷输出 18 fps（CPU 渲染，无显卡） | 性能预算见 §12，以空闲 0% CPU 为硬指标 |
| SSH + 同步输入 | `SyncConn` 包住 `Conn` 就够了；终端自己的应答（光标位置等）必须过滤（`StripReplies`）；写错误会永久关闭终端的输入队列，所以重连期间 `Write` 永远不能返回错误 | `session.Pane` 沿用这套约定 |
| 小地图 | 点击、框选、拖动重排、角落吸附、透明度滑块都做出来了；**分隔线吸附没做**；`ui.Split` 没有吸附点 | 分隔线用 `Draggable` 自己做（M2） |
| 打包 | 三平台从 Linux 一次交叉编译成功（Linux 13 MB、Windows 14 MB、macOS 通用 30 MB） | 发布流水线按 MyGo 文档：macOS 在 macOS runner 上签名公证 |
| **没验证** | 真实 OpenSSH、跳板机、agent、`~/.ssh/config`、GPU 渲染、Windows/macOS 真机运行、macOS 签名公证、NSIS 安装包、自动更新 | M1 起每个里程碑都补一块，见 §13 |

---

## 2. 架构风格与依赖注入

**风格：扁平的“端口与适配器”**，不上 DDD、不分 service/repository 层（项目规模和这个需求不需要）。规则只有三条：

1. 依赖方向：`ui → app → (workspace, session, transfer, monitor, hostlib, settings, …) → (transport, store, secrets 接口)`。核心包**不得**导入 `internal/ui`，也不得导入 MyGo（`secrets`、`platform` 除外）。用 `golangci-lint` 的 `depguard` 在 CI 里强制。
2. 接口放在**使用方**，保持小（`Transport`、`Collector`、`SecretStore`、`Clock`、`HostKeyPrompter`）。
3. 装配只在 `internal/app` 里做，**手写构造函数注入**，不用 DI 库（对象不到 20 个，生命周期清楚）。【待确认】项目布局技能要求先问架构和 DI 偏好，这里取上面的默认。

Go 风格要点（来自两套规范，写进代码评审清单）：

- 现代写法：`wg.Go`、`for i := range n`、`min`/`max`、`slices`/`maps`、`iter.Seq`、`errors.AsType`、`t.Context()`、`b.Loop()`、JSON 标签 `omitzero`。每次改 Go 文件前先跑 `use-modern-go` 的 `list`。
- 错误：用 `%w` 包装、`errors.Is/As`；对外暴露有限的**哨兵错误与类型化错误**；一条错误只在一处处理（要么返回、要么记录）。日志用标准库 `log/slog`，结构化，**永不记录秘密**。
- 并发：每个 goroutine 都有明确的所有者和退出条件（`context` + `wg.Go`），goroutine 泄漏用 `goleak` 测。
- 注释：导出的标识符都有 doc 注释；包注释写“这个包负责什么、不负责什么、线程模型”（原型已是这个风格）。

---

## 3. 模块与目录

模块路径：`github.com/wei-dong/tako`（原型是 `github.com/wei-dong/tako-spike`）。【待确认：仓库名与 GitHub 账号】

```
tako/
├─ go.mod  go.sum  mygo.json  Makefile  .golangci.yml  .gitignore
├─ .github/
│  ├─ workflows/ci.yml  release.yml
│  └─ dependabot.yml
├─ cmd/tako/main.go                 # 解析参数、调用 app.Run()，不放逻辑
├─ resources/
│  ├─ icons/                        # 应用图标（章鱼）、托盘
│  ├─ fonts/                        # JetBrains Mono（随应用打包）
│  └─ locales/zh-Hans.json  en.json # 文案
├─ internal/
│  ├─ app/                          # 装配根：创建各服务、窗口、菜单、全局快捷键
│  ├─ model/                        # 纯数据类型：Host、Tag、Auth、Hop、Proxy、OSKind、ID
│  ├─ hostlib/                      # 主机库：增删改、标签、搜索、导出（不含秘密）
│  ├─ importer/                     # sshconfig、xshell、securecrt、termius（纯解析，返回 []model.Host）
│  ├─ secrets/                      # SecretStore 接口 + 钥匙串实现（mygo.Secrets）+ 内存假实现
│  ├─ transport/                    # Transport 接口；ssh/（认证、跳板、代理、known_hosts）；local/（本机 PTY）
│  ├─ session/                      # Conn 池、Pane（paneConn）、重连状态机、同一主机多通道复用
│  ├─ inputbus/                     # 同步输入扇出 + StripReplies（沿用原型）
│  ├─ workspace/                    # Model：块、标签、同步、焦点；命令（打开/关闭/移动/放大）
│  ├─ layout/                       # 纯几何：网格、分割树、吸附、小地图命中与框选（沿用并扩展原型）
│  ├─ monitor/                      # Collector 接口；linux/ darwin/ windows/ 解析器；Sampler
│  ├─ transfer/                     # Job、Queue、中转、直连、续传、校验
│  ├─ store/                        # JSON 持久化：原子写、版本迁移、会话恢复
│  ├─ settings/                     # 设置结构、默认值、校验
│  ├─ i18n/                         # 对 MyGo i18n 的薄封装 + 键名常量
│  ├─ theme/                        # 设计 token（Light / Dark），与 DESIGN.md 同名
│  ├─ platform/                     # 少量系统相关：外观跟随、快捷键映射 ⌘/Ctrl、打开文件管理器
│  ├─ ui/                           # MyGo 视图：shell、sidebar、grid、block、minimap、sheet、palette、monitor、transfer
│  └─ testutil/                     # 内存 SSH 服务器（原型的 testsshd）、假 Collector、假钥匙串
└─ test/
   └─ integration/                  # docker-compose：真实 OpenSSH 矩阵（见 §11）
```

要点：

- 原型里的 `sshconn` 拆成 `transport/ssh`（建立连接）和 `session`（Pane、重连）两层：原型把“建连、重连、keepalive、SFTP”都放在一个 397 行的包里，产品要加跳板、代理、多通道复用，必须拆。
- `layout` 保持**零依赖**（原型已如此），所有吸附、命中、框选都用单元测试和模糊测试覆盖，UI 只负责画。
- `importer/*` 是纯函数：`func Parse(r io.Reader) ([]model.Host, []Warning, error)`，不碰钥匙串、不碰网络，方便模糊测试。

---

## 4. 核心类型与接口

> 以下是接口草图，目的是定边界，不是最终代码。

### 4.1 `model`：数据

```go
package model

type ID string // 形如 "h_3f9a…"，创建时生成，永不复用

type OSKind string // "linux" | "macos" | "windows"

type Host struct {
	ID       ID        `json:"id"`
	Name     string    `json:"name"`
	Addr     string    `json:"addr"`            // 主机名或 IP
	Port     int       `json:"port,omitzero"`   // 0 = 22
	User     string    `json:"user"`
	OS       OSKind    `json:"os"`              // 决定监控采集方式；可由“测试连接”自动识别
	Tags     []string  `json:"tags,omitzero"`
	Auth     []Auth    `json:"auth"`            // 按顺序尝试
	Jump     []Hop     `json:"jump,omitzero"`   // ProxyJump，多级
	Proxy    *Proxy    `json:"proxy,omitzero"`  // SOCKS5 / HTTP
	StartDir string    `json:"startDir,omitzero"`
	StartCmd string    `json:"startCmd,omitzero"`
	Encoding string    `json:"encoding,omitzero"` // "" = UTF-8，"gbk"
	KeepAlive Duration `json:"keepAlive,omitzero"`
	HostKeyPolicy string `json:"hostKeyPolicy,omitzero"` // "ask"（默认）| "strict"
	Note     string    `json:"note,omitzero"`
}

type AuthKind string // "password" | "key" | "agent" | "keyboard-interactive"

type Auth struct {
	Kind    AuthKind `json:"kind"`
	KeyPath string   `json:"keyPath,omitzero"`  // kind=key
	// 秘密不在这里。口令/密码以“钥匙串引用”形式存在：
	SecretRef string `json:"secretRef,omitzero"` // 如 "host/h_3f9a/password"；空 = 每次询问
}

type Hop struct { HostID ID `json:"hostId"` } // 跳板机本身也是主机库里的一台主机
type Proxy struct { Kind string `json:"kind"`; Addr string `json:"addr"`; SecretRef string `json:"secretRef,omitzero"` }
```

### 4.2 `transport`：怎么连

```go
// Transport 是一条已建立的、可开多个通道的连接（SSH 或本机）。
type Transport interface {
	// OpenShell 在远端开一个带 PTY 的交互 shell。
	OpenShell(ctx context.Context, size Size) (Shell, error)
	// Exec 在一个独立通道里执行命令（监控用，不占 PTY）。
	Exec(ctx context.Context, cmd string) ([]byte, error)
	// SFTP 返回 SFTP 客户端（传输用）。
	SFTP(ctx context.Context) (SFTPClient, error)
	Close() error
	// Done 在底层连接断开时关闭。
	Done() <-chan struct{}
	Err() error
}

type Dialer interface {
	Dial(ctx context.Context, h model.Host) (Transport, error)
}

type Shell interface { io.ReadWriteCloser; Resize(cols, rows int) error }

// 依赖的外部事物都用接口，方便测试：
type HostKeyPrompter interface { // UI 实现：弹出指纹确认
	Confirm(ctx context.Context, host string, key ssh.PublicKey, changed bool) (accept bool, err error)
}
type SecretStore interface { // internal/secrets
	Get(ctx context.Context, ref string) ([]byte, error)
	Set(ctx context.Context, ref string, value []byte) error
	Delete(ctx context.Context, ref string) error
}
```

`transport/ssh` 负责：认证链（密码 / 密钥含口令与 .ppk / ssh-agent / keyboard-interactive）、多级 ProxyJump（逐跳 `ssh.NewClientConn` 套在前一跳的 `Dial` 上）、SOCKS5 / HTTP 代理、`known_hosts` 校验、keepalive。**没有“忽略主机密钥”的选项**（沿用原型的 `ErrHostKeyRequired` 约定）。

### 4.3 `session`：连接池、Pane、重连

```go
// Session 是一台主机的连接：一条 Transport，上面开多个通道（终端标签、监控、SFTP）。
// 遇到服务器 MaxSessions 限制时自动再建一条 Transport。
type Session struct { /* host, dialer, pool, state, subscribers */ }

func (s *Session) OpenPane(ctx context.Context, size transport.Size) (*Pane, error)
func (s *Session) Exec(ctx context.Context, cmd string) ([]byte, error)
func (s *Session) SFTP(ctx context.Context) (transport.SFTPClient, error)
func (s *Session) State() State                 // Connected | Reconnecting{Attempt, NextRetry} | Closed
func (s *Session) Watch() iter.Seq[StateChange] // UI 订阅
func (s *Session) Reconnect()                   // “立即重连”

// Pane 满足 terminal.Options.Conn 需要的 io.ReadWriteCloser + Resize。
type Pane struct { /* 当前 Shell、输出通道、丢弃计数 */ }
func (p *Pane) Read(b []byte) (int, error)   // 重连期间阻塞，不返回 EOF
func (p *Pane) Write(b []byte) (int, error)  // 重连期间丢弃并计数，永不返回错误
func (p *Pane) Resize(cols, rows int) error
func (p *Pane) Dropped() uint64              // 断线期间被忽略的输入字节数
```

### 4.4 `workspace`、`inputbus`

- `inputbus` 原样沿用原型：`Bus.Register / SetSynced / Publish / Stats`，每个目标一个 goroutine + 有界队列，溢出丢弃并计数；`StripReplies` 过滤终端自己的应答；`SyncConn` 包住 `Pane`。
- `workspace.Model` 持有 `[]*Block`；`Block{ID, HostID, Tabs, Active, Synced}`；`Tab{ID, Title, Pane, Cwd}`。**所有修改只在 UI 线程**（原型的约定），总线 goroutine 只读 `atomic.Pointer` 指向的当前标签。
- 命令（`OpenHost`、`CloseBlock`、`MoveBlock`、`ToggleSync`、`ZoomBlock` 等）都是 `Model` 的方法，**命令面板、快捷键、右键菜单、点击共用同一批方法**，保证“同一操作多处入口”行为一致（对应 DESIGN.md 的“操作→唯一可见入口”表）。

### 4.5 `monitor`：三系统采集

```go
type Sample struct {
	At       time.Time
	CPU      float64            // 0–100，总体
	CPUCores []float64          // 可选
	Load1    float64
	MemUsed, MemTotal uint64
	Disks    []DiskUsage        // 挂载点、已用、总量
	DiskIO   IOStat             // 读写字节/秒
	Net      NetStat            // 上下行字节/秒
	GPUs     []GPUSample        // 利用率、显存、温度、功耗；没有则为空
}

// Collector 把“要执行的命令”和“怎么解析输出”拆开：纯函数，易测。
type Collector interface {
	Command() string                          // 一次 Exec 取回全部指标，用分隔符隔开
	Parse(out []byte, prev *Raw) (Sample, Raw, error)
}

func ForOS(os model.OSKind, probes Probes) Collector // linux / darwin / windows
```

- **Linux**：一条命令读 `/proc/stat`、`/proc/meminfo`、`/proc/diskstats`、`/proc/net/dev`、`/proc/loadavg` 和 `df -P`；CPU 与 IO、网络都靠**两次采样相减**，所以 `Parse` 接收上一次的原始计数；GPU 用 `nvidia-smi --query-gpu=… --format=csv,noheader,nounits`，失败再试 `rocm-smi --json`，都没有就不显示 GPU。
- **macOS**：`top -l 1 -n 0`、`vm_stat`、`iostat -d -c 2`、`df -P`、`netstat -ib`。
- **Windows**：一条 PowerShell（`-NoProfile -NonInteractive -OutputFormat Text`）输出 JSON：`Get-CimInstance Win32_Processor/OperatingSystem/LogicalDisk`、`Get-Counter` 的网络和磁盘。需要注意 OpenSSH for Windows 默认 shell 可能是 cmd，命令要带 `powershell -Command`。
- `Sampler`：每台主机一个 goroutine，用 `time.Ticker` 按设置间隔（默认 2 s，加 ±10% 抖动，避免 9 台同时发）；连接断开时暂停，恢复后重置“上一次计数”；结果通过 `iter.Seq` / 回调推给 UI；UI 只在数值变化时刷新（MyGo 空闲不绘制，不能每秒强制重绘整窗）。

### 4.6 `transfer`：传输作业

```go
type Endpoint struct { HostID model.ID /* 空 = 本机 */; Path string }

type Job struct {
	ID       ID
	Kind     Kind          // Upload | Download | Relay | Direct
	Src, Dst Endpoint
	Size     int64
	State    State         // Queued | Running | Paused | Failed | Done
	Done     int64         // 已传字节，续传的偏移量
	Err      error
}

type Queue struct{ /* 并发数、事件订阅 */ }
func (q *Queue) Add(ctx context.Context, spec Spec) (*Job, error)
func (q *Queue) Pause(id ID) ; Resume(id ID) ; Cancel(id ID)
func (q *Queue) Watch() iter.Seq[Event]
```

- **中转（默认）**：A→本机→B 流水线（`io.Pipe` 或固定大小缓冲，边下边上，**不落盘**），一个 `errgroup`；断点续传按**目标端已有字节数**从偏移处继续（`SFTP Stat` 取大小，`Seek` 两端），完成后做大小 + 可选哈希校验。
- **直连（可选）**：在 B 上 `exec` `scp` / `rsync` 拉取 A，使用**临时 agent 转发**，不把私钥放到服务器；需要用户在设置里显式开启。
- 目录：先递归枚举，再逐文件入队；同名文件的覆盖 / 跳过 / 询问由设置决定。
- 并发默认 3，设置可调；单个 Job 内部串行，避免打满带宽后互相饿死。

### 4.7 `store` 与 `settings`、`i18n`、`theme`

见 §5、§9、§10。

---

## 5. 数据存储

### 5.1 位置与文件

| 内容 | 位置（`os.UserConfigDir()` / `os.UserStateDir()` 下的 `Tako/`） | 文件 | 是否可导出 |
|---|---|---|---|
| 主机库 | config | `hosts.json` | 是（不含秘密） |
| 设置 | config | `settings.json` | 是 |
| 布局预设 | config | `layouts.json` | 是 |
| 主机密钥 | config | `known_hosts`（OpenSSH 格式，Tako 自己的一份；设置里可选“同时读取 `~/.ssh/known_hosts`”） | 否 |
| 会话恢复 | state | `session.json` | 否 |
| 日志 | state | `logs/tako-YYYYMMDD.log`（`slog` JSON，滚动，保留 7 天） | 否 |
| 秘密 | **系统钥匙串**（服务名 `dev.tako.app`） | — | **永不** |

macOS 实际路径是 `~/Library/Application Support/Tako/`，Windows 是 `%AppData%\Tako\`，Linux 是 `~/.config/tako/`（配置）与 `~/.local/state/tako/`（状态）。

### 5.2 写入规则

- **原子写**：写临时文件 → `fsync` → `rename`；权限 `0600`（目录 `0700`）。
- **版本**：每个文件顶层 `"version": N`；读到旧版本时先备份 `*.bak.vN` 再逐级迁移；读到**更新的版本**时拒绝覆盖，提示升级应用。
- **容错**：JSON 损坏时不静默清空，保留损坏文件为 `*.corrupt-<时间>` 并从上一次备份（每次启动轮转 3 份）恢复。
- **并发**：只有一个写入 goroutine；变更先改内存模型，再去抖 300 ms 落盘；退出时同步刷新。
- **单实例**：用锁文件防止两个 Tako 同时写同一份配置（第二个实例把窗口交给第一个）。

### 5.3 `hosts.json`

```json
{
  "version": 1,
  "hosts": [
    {
      "id": "h_8c1f2a", "name": "web-01", "addr": "10.0.1.11", "port": 22, "user": "ubuntu",
      "os": "linux", "tags": ["生产", "nginx"],
      "auth": [{ "kind": "key", "keyPath": "~/.ssh/id_ed25519", "secretRef": "host/h_8c1f2a/key-passphrase" }],
      "jump": [{ "hostId": "h_bastion" }],
      "hostKeyPolicy": "ask"
    }
  ],
  "tags": [
    { "name": "生产", "color": "#E39A12" },
    { "name": "GPU 集群", "color": "#0FA3B1" }
  ]
}
```

- `secretRef` 只是钥匙串里的键名，**没有任何口令内容**。导出时 `secretRef` 也被去掉（换电脑后会提示“请重新输入”）。
- 钥匙串键名固定为 `host/<hostId>/<kind>`、`proxy/<hostId>`；删除主机时一并删除对应条目。

### 5.4 会话恢复 `session.json`

```json
{
  "version": 1,
  "savedAt": "2026-11-11T14:32:08+08:00",
  "window": { "x": 80, "y": 60, "w": 1440, "h": 900, "maximized": false, "sidebarCollapsed": false },
  "layout": {
    "mode": "manual",
    "tree": { "split": "row", "ratio": 0.5,
      "a": { "block": "b1" },
      "b": { "split": "col", "ratio": 0.333, "a": { "block": "b2" }, "b": { "block": "b3" } } }
  },
  "blocks": [
    { "id": "b1", "hostId": "h_8c1f2a", "synced": true, "active": 0,
      "tabs": [ { "id": "t1", "title": "zsh", "cwd": "/srv/app" }, { "id": "t2", "title": "logs", "cwd": "/var/log/nginx" } ] }
  ],
  "minimap": { "visible": true, "corner": "br", "opacity": 0.55 },
  "focus": "b1"
}
```

- **自动布局**时 `layout` 只存 `{"mode":"auto","order":["b1","b2",…]}`；**手动布局**存二叉分割树（叶子是块 ID，内部节点是方向 + 比例）。
- 每次块 / 标签 / 同步 / 布局变化后去抖 1 s 写盘，退出时写一次。**崩溃后**也能恢复到最近 1 秒内的状态。
- **不保存终端回滚内容**（隐私与体积）；恢复时重新连接，并在设置开启时用 OSC 7 记录的 `cwd` 做 `cd`。恢复失败的块显示断线横幅，不阻塞其他块。
- 恢复是**并行**的：每个块独立连接，先出界面再逐个连上。

---

## 6. 并发模型

| goroutine | 数量 | 所有者 | 退出条件 |
|---|---|---|---|
| UI 主线程（MyGo） | 1 | MyGo | 窗口关闭 |
| `Bus` 写入协程 | 每个已注册块 1 个（有界队列 1024） | `inputbus.Bus` | `Unregister` / `Close` |
| 重连协程 | 每台主机 1 个 | `session.Session` | 连上 / 用户关闭 / `ctx` 取消 |
| keepalive | 每个 `Transport` 1 个 | `transport/ssh` | `Transport` 关闭 |
| 输出泵 | 每个 `Pane` 1 个（Shell → 终端） | `session.Pane` | `Pane` 关闭 |
| 监控采样 | 每台已打开主机 1 个 | `monitor.Sampler` | 主机无块 / 断线 / 退出 |
| 传输工作协程 | 设置的并发数（默认 3） | `transfer.Queue` | `Queue.Close` |
| 持久化写入 | 1 | `store` | 应用退出（先刷盘） |

原则：

- **UI 数据只在 UI 线程改**；后台 goroutine 通过 MyGo 提供的“回到主线程”的机制投递结果（具体 API 在 M1 开工时核对）。
- **跨 goroutine 的共享状态**只有两类：`Bus`（读写锁 + `atomic`）和 `Block` 的“当前可见标签”（`atomic.Pointer`），都沿用原型。
- **`context` 贯穿**：应用根 `ctx` → `Session` → `Pane` / `Exec` / `SFTP`；每个阻塞调用都带超时或可取消；后台工作用 `context.WithoutCancel` 的场合（例如“退出时刷盘”）要写注释。
- **用 `wg.Go` 和 `errgroup`**，不手写 `go func(){…}()` 而没人等；每个包有 `goleak` 测试。
- **背压**：终端输出由 MyGo 终端自己节流；总线队列满了丢弃并计数（UI 可显示“某台漏了输入”）；传输是流式，缓冲固定（默认 256 KiB）。
- **退出顺序**：停止接受新命令 → 关闭监控与传输（保存续传偏移）→ 写 `session.json` → 关闭 `Pane` → 关闭 `Transport` → `Bus.Close()`。

---

## 7. 错误处理与重连

### 7.1 错误分类

```go
var (
	ErrHostKeyRejected = errors.New("host key rejected")
	ErrAuthFailed      = errors.New("authentication failed")
	ErrUnreachable     = errors.New("host unreachable")
	ErrProxyFailed     = errors.New("proxy or jump host failed")
)
type HostKeyChangedError struct{ Host string; Old, New string /* 指纹 */ }
```

- **可重试**：网络断开、超时、对端复位 → 进入重连。
- **不可重试**：认证失败、主机密钥变化、被用户拒绝 → 停止自动重连，块里显示具体原因和对应操作（重新输入密码 / 查看指纹 / 编辑主机）。
- UI 文案由错误类型映射到 i18n 键，**不把底层错误字符串直接给用户**，但会提供“复制详情”。

### 7.2 重连状态机（`session.Session`）

```
Connected ──(Transport.Done)──▶ Reconnecting(attempt=1) ──成功──▶ Connected
                                   │  失败: 等待 backoff[attempt]
                                   ├─(不可重试错误)──▶ Failed(原因)   // 等待用户操作
                                   └─(用户关闭块)────▶ Closed
```

- 退避：`1 s → 2 s → 5 s → 10 s → 30 s`，之后每 30 s 一次，**不放弃**（设置里可限制次数）；加 ±20% 抖动。多台同时断线（网络整体掉线）时，用全局 `semaphore` 限制并发重连数，避免“惊群”。
- 重连成功后：重新开 Shell、`Resize` 到当前尺寸、重建监控与 SFTP 通道；**不补发**断线期间的输入（规格与定稿都明确）。
- 用户“立即重连”：跳过当前等待。
- 同步状态：断线块的同步开关显示“已暂停”，总线对它的队列**清空并丢弃**；重连后自动恢复同步状态。

### 7.3 日志

- `slog`，JSON 行；字段统一：`host`、`block`、`tab`、`op`、`attempt`、`err`。
- **脱敏**：自定义 `slog.Handler`，对键名含 `pass`、`secret`、`key`、`token` 的字段一律替换为 `[redacted]`；终端内容**绝不入日志**。

---

## 8. 安全

| 主题 | 做法 |
|---|---|
| **主机密钥** | 首次连接弹出指纹（SHA256 + 随机艺术图可选）供确认，写入 Tako 的 `known_hosts`；**指纹变化 = 拒绝连接**，需用户在主机详情里“信任新密钥”才能继续（原型已验证重连时不会自动放行）；没有“忽略校验”的开关 |
| **秘密存储** | 密码、私钥口令、代理密码只进系统钥匙串（`mygo.Secrets`，服务名 `dev.tako.app`）；内存里只在连接握手期间持有，用后清零 `[]byte`；私钥文件**不拷贝**，只记路径 |
| **导出与备份** | 导出的 JSON 不含 `secretRef` 与任何秘密；导入后缺失的秘密在首次连接时询问；`session.json`、日志不进导出 |
| **私钥口令缓存** | 可选“本次运行记住”，仅内存，退出即清；不写盘 |
| **agent** | 只读使用系统 `SSH_AUTH_SOCK`（含 1Password / Bitwarden 的 agent）；agent 转发（直连传输用）**默认关闭**，只在单次直连传输期间临时开启 |
| **算法** | 使用 `x/crypto/ssh` 的默认安全算法集，不主动启用已废弃算法；老设备需要时在主机设置里逐台开启并标注风险 |
| **同步输入的风险** | 同步会把你敲的**每个字符**（包括 `sudo` 的密码）发给所有同步主机。**定稿决定不加危险命令确认**，所以这里只做低成本缓解：检测到终端进入“无回显”状态（密码提示）时，**当次输入暂停同步**并在块头短暂提示。【待确认：要不要做？】 |
| **本地攻击面** | 配置目录 `0700`、文件 `0600`；`session.json` 不含秘密；不开监听端口；自动更新用 MyGo 的**签名更新**，校验失败拒绝安装 |
| **依赖** | `govulncheck` 进 CI；`go.sum` 锁定；Dependabot 每周；`gosec` 进 lint |
| **输入校验** | 导入器、ssh_config 解析、CSV 解析都做**模糊测试**，对超长行、嵌套 `Include`（限制深度 8）、环状 ProxyJump 做防护 |

---

## 9. 国际化（中 / 英）

- 使用 MyGo 自带的 `i18n` 与 `locale` 机制（`SetLanguages`、`SetLocale`；默认跟随系统首选语言，找不到匹配时回退到 `zh-Hans`）。
- 文案放 `resources/locales/zh-Hans.json`、`en.json`；**键名按位置命名**：`sidebar.addHost`、`block.sync.on`、`disconnect.banner.reconnecting`、`transfer.queue.paused`……代码里用 `internal/i18n` 导出的**常量**，拼错键名编译期就报错。
- 不拼接句子：带变量的写成完整模板（`disconnect.banner.reconnecting`: “连接已断开 · 重连中（第 {attempt} 次）”），复数用 MyGo 的复数规则。
- **测试**：键对齐测试（两种语言键集合必须一致）；“伪语言”渲染测试（`[!! 断线 !!]`，长度 +30%）检查控件是否被截断；日期、数字、文件大小用 `Region` 格式。
- 效果图是中文；英文版在 M4 前**每个新增文案都同步写英文**，避免最后一次性翻译。
- 不翻译：主机名、命令、路径、终端内容。

---

## 10. 主题与设计 token

- `internal/theme` 提供 `Tokens` 结构，**字段名与 `DESIGN.md` §2.1 的 CSS 变量一一对应**（`Bg`、`Sidebar`、`Surface`、`Surface2…4`、`Border`、`BorderStrong`、`Text`、`Text2`、`Text3`、`Accent`、`AccentSoft`、`AccentRing`、`Green`、`Amber`、`Red`、`Blue`、`Purple`、`Teal`、`TermBg`、`TermFg`、`TermDim`），以及定稿新增的 `SyncBorder`、`Glass2`、`Glass3`（定义在 `src/final.css` / `src/lib3.js`，M1 前需补进 `DESIGN.md` §2.1 的表），以及尺寸常量（侧栏 248、块头 38、块脚 32、状态栏 28、圆角 6/8/12/14/16、间距基数 4）。
- `Light` 与 `Dark` 两份值直接抄 `DESIGN.md` 的表；**一个测试**把 `theme` 里的值与 `DESIGN.md` 表格解析出的值逐项比对，防止设计和代码漂移。
- 外观跟随系统（`platform` 读取并订阅变化），设置里可强制；切换时只换 token，不重建控件树。
- 终端配色独立于应用主题（设置里选内置方案或导入 iTerm2 配色）；默认随应用明暗切换。
- 字体：JetBrains Mono 随应用打包（`resources/fonts`）；UI 字体用系统字体（macOS PingFang SC、Windows 微软雅黑 UI、Linux Noto Sans CJK）。
- 毛玻璃（小地图静止态）用 MyGo 的 `plugins/glass`；**原型没试过**，M2 第一件事是验证它在三平台的效果，不行就退回“纯半透明 + 阴影”。

---

## 11. 测试策略

| 层 | 内容 | 工具 | 目标 |
|---|---|---|---|
| **单元** | `layout`（网格、分割树、吸附、命中、框选）、`inputbus`、`StripReplies`、退避计算、`model` 校验、`store` 迁移、`monitor` 解析器（用**真实机器抓来的输出样本**放 `testdata/`：Linux `/proc` 快照、macOS `top`/`vm_stat`、Windows PowerShell JSON、`nvidia-smi` CSV）、导入器 | 标准 `testing`、表驱动、`t.Context()`、`-race` | 核心包覆盖率 ≥ 80% |
| **模糊** | `StripReplies`、ssh_config / Xshell / SecureCRT / Termius 导入器、`store` 读取、路径与 OSC 7 解析 | `go test -fuzz`，语料入库 | CI 每次跑 30 秒，夜间跑 10 分钟 |
| **属性** | `layout`：任意操作序列后块不重叠、总面积守恒、吸附幂等 | 自写生成器 / `testing/quick` | — |
| **UI** | 用 MyGo 的 `ui.Tester` 无窗口跑真实控件树：小地图（点击、框选、拖动、吸附、透明度）、块头（同步 chip、标签页）、侧栏、弹层、命令面板、断线横幅、空状态；少量**截图对比**（每屏 1–2 张，容差内比对，基线图随 `DESIGN.md` 效果图更新） | `ui.Tester` | 每个里程碑的验收场景都有对应测试 |
| **集成** | **真实 OpenSSH**（Docker）：见下 | `docker compose` + `go test -tags=integration` | M1 起每次 PR 跑 |
| **端到端** | 原型的 `internal/e2e` 升级：真实控件 + 内存 SSH 服务器 + 总线 | `ui.Tester` | 同步、重连、重排 |
| **并发** | `-race -count=3`、`goleak` | — | 零数据竞争、零泄漏 |
| **性能** | 沿用原型的 `BenchmarkGridFrame*`；CI 记录趋势，超过预算（§12）+20% 失败 | `b.Loop()` | — |
| **真机手测** | Windows 与 macOS 的打包产物在真机上跑冒烟清单；Windows / macOS **主机**（被连接端）的监控与传输对真实机器验证 | 清单 | 每个里程碑发布前 |

**真实 OpenSSH 集成矩阵（`test/integration`）**：`docker compose` 起一个自带 `sshd`、PAM、`google-authenticator` 的镜像和若干容器：

| 场景 | 做法 |
|---|---|
| 密码、公钥、带口令的私钥（OpenSSH / PEM / .ppk） | 预置用户与密钥 |
| ssh-agent | 测试进程内起 agent，加载密钥 |
| 键盘交互 + TOTP | PAM + `google-authenticator`，测试里用固定种子算验证码 |
| 多级 ProxyJump | 堡垒机容器 + 仅内网可达的目标容器 |
| SOCKS5 / HTTP 代理 | `microsocks`、`tinyproxy` 容器 |
| 断线重连 | `docker pause` / `docker restart` / `tc` 丢包，断言 `Pane.Read` 不返回 EOF、回滚保留 |
| 主机密钥变化 | 重建容器换主机密钥，断言拒绝重连 |
| `MaxSessions 1` | 断言自动新建第二条连接 |
| SFTP 与续传 | 中途 `docker kill` 再恢复，断言从偏移处继续、校验通过 |
| 中转 / 直连互传 | 两个目标容器 |
| 大输出与慢目标 | 一台目标限速，断言不拖慢其他同步目标（总线隔离） |

> Windows / macOS 作为**被连接主机**，Docker 里没法模拟：解析器靠样本测试，真机验证需要真实的 Windows（开 OpenSSH Server）和 macOS（开启远程登录）机器。【待确认：是否有这样的机器可用于验证？】

---

## 12. CI、打包与性能预算

### 12.1 持续集成（GitHub Actions）

`ci.yml`（PR 与 `main`）：

1. **lint**（Ubuntu）：`go vet`、`golangci-lint`（按 `golang-lint` 技能的配置：`errcheck`、`govet`、`staticcheck`、`revive`、`gosec`、`errorlint`、`gocritic`、`depguard`（强制 §2 的依赖方向）、`forbidigo`（禁用 `fmt.Print*` 和 `log.*`）、`gofmt`）、`govulncheck`。
2. **test**（Ubuntu + macOS + Windows 三个 runner）：`go test -race -count=1 ./...`（`ui.Tester` 无窗口，三平台都跑）。
3. **integration**（Ubuntu）：`docker compose up` 后跑 `-tags=integration`。
4. **fuzz-smoke**：每个模糊目标 30 秒。
5. **bench**（仅 `main`）：记录 `BenchmarkGridFrame*`，对比上一次，超预算 20% 失败。
6. **build-all**：`mygo build` 三平台，`-skip-installer`，上传构件，检查体积预算。

`release.yml`（打 `v*` 标签）按 MyGo 的 `docs/github-actions.md`：先建草稿 Release，然后三个 job 并行——

| 平台 | Runner | 产物 | 签名 / 公证 |
|---|---|---|---|
| macOS（arm64 + amd64，或 universal） | `macos-latest` | `.app` + `.dmg` | Developer ID 签名（hardened runtime）+ `notarytool` 公证 + 装订；证书用 base64 存 Secrets，导入临时钥匙串；`signingIdentity` 取自环境变量 |
| Windows（amd64 + arm64） | `ubuntu-latest`（MyGo 无需 cgo，可在 Linux 上构建） | `.exe` + NSIS 安装包 | `certificate` 或 `signCommand`（推荐云签名服务，避免把证书放进仓库） |
| Linux（amd64 + arm64） | `ubuntu-latest` | 二进制、`.deb`、`.rpm`、`tar.gz`、`install.sh`、PKGBUILD | 包签名可选 |

- 自动更新用 MyGo 的签名更新（含增量更新），更新源用 GitHub Releases；更新密钥只放 Secrets。
- 版本号 `vMAJOR.MINOR.PATCH`；`internal/app` 里的版本由 `-ldflags` 注入。
- `Makefile`：`make test race lint fuzz integration bench build build-all`，与原型一致，CI 只是调用它们。

### 12.2 性能预算（依据原型实测）

| 指标 | 预算 | 原型实测 / 依据 |
|---|---|---|
| 空闲 CPU（1–9 块） | **0%**（MyGo 只在内容变化时绘制，任何“每秒重绘”的做法都不允许） | 0% |
| 空闲内存（9 块 SSH） | ≤ 100 MB RSS | 66 MB |
| 9 块正常使用（每块偶尔输出） | ≤ 150 MB | 原型未测，M1 测 |
| 9 块**同时狂刷输出**（压力上限） | ≥ 18 fps（CPU 渲染）；有显卡 ≥ 30 fps | 18 fps，平均帧 46.6 ms，RSS 358 MB（无显卡） |
| 6 块狂刷（SSH，CPU 渲染） | ≥ 30 fps、平均帧 ≤ 33 ms | 31 fps / 26.1 ms，p95 42.6 ms |
| 整窗一次全变化 | ≤ 12 ms（9 块） | 10.4 ms |
| 空闲帧 | ≤ 0.2 ms | 0.167 ms |
| **回滚行数默认值** | **10 000 行**（规格），并按字节再设上限（待验证，见 §13 风险 7） | 原型的默认是每终端 10 MB，是内存大头，已下调 |
| 同步输入扇出延迟（进程内） | ≤ 5 ms（9 目标） | 原型测试覆盖，未量化 |
| 监控开销（被监控端） | 每 2 秒一次 `exec`，被监控端 CPU < 1%，解析 < 1 ms | 新增，M3 测 |
| 冷启动到出窗口 | ≤ 1 s（不含网络连接） | 新增，M1 测 |
| 传输吞吐 | ≥ 链路上限的 80% | 新增，M3 测 |
| 二进制体积 | Linux ≤ 20 MB、Windows ≤ 20 MB、macOS 通用 ≤ 40 MB | 13 / 14 / 30 MB（含新功能后预算放宽） |

---

## 13. 原型遗留的风险与应对

| # | 风险 | 影响 | 应对 | 何时消除 |
|---|---|---|---|---|
| 1 | **MyGo 仍是 0.x**（v0.8.3），API 可能变化 | 升级时编译失败、行为变化 | `go.mod` 固定版本；升级单独开 PR 并跑全套测试；把对 MyGo 的依赖集中在 `ui`、`secrets`、`platform`，核心包不碰 | 持续 |
| 2 | **GPU 渲染性能没测**（原型只有 CPU 渲染） | 9 块刷屏时在真机上的帧率未知 | M1 在有显卡的 Mac 和 Windows 机器上测预算表；不达标则降低刷新率或合并重绘 | M1 末 |
| 3 | **Windows / macOS 产物从没在真机运行过** | 启动、字体、快捷键、钥匙串、外观跟随都可能有平台差异 | M1 起每个里程碑在两台真机上跑冒烟清单；CI 的三平台 `ui.Tester` 先挡住一部分 | M1 末起 |
| 4 | **真实 OpenSSH 兼容性**（跳板机、agent、键盘交互、ssh_config）原型都没做 | 实际服务器上连不上 | M1 的 Docker 集成矩阵（§11） | M1 |
| 5 | **分隔线吸附没有现成实现**（`ui.Split` 无吸附点） | 工作量与手感 | 用 `Draggable` 自己做，吸附逻辑放进零依赖的 `layout` 包，用属性测试保证；先做“比例吸附到 1/2、1/3、2/3”，再做“与相邻行列对齐” | M2 |
| 6 | **毛玻璃（`plugins/glass`）没试过** | 小地图静止态半透明的观感与性能 | M2 第一周先验证三平台；备选方案是纯半透明 + 阴影 | M2 |
| 7 | **回滚内存**：默认 10 MB / 终端是内存大头，规格要求 10 000 行 | 9 块刷屏时内存涨 | 核对终端插件是否能同时限制行数和字节；默认 10 000 行 + 每终端 ≤ 4 MB；设置里可调；监控“RSS 随输出量而不是终端数量增长”这一点 | M1 |
| 8 | `StripReplies` 是**启发式**，可能漏过滤或误伤 | 多台主机互相应答、或吞掉用户真输入 | 保持保守（只过滤已知应答序列）；模糊测试；M2 增加“同步诊断”日志开关 | M2 |
| 9 | **GBK 等非 UTF-8 编码**：规格有，但终端插件是否只吃 UTF-8 未知 | 乱码 | 在 `session.Pane` 里做编码转换层（`golang.org/x/text/encoding`），先在 M1 验证 UTF-8 路径，GBK 放 M4 | M4 |
| 10 | **中文输入法（IME）** 在终端控件里的表现 | 候选框位置、组合态 | M1 在 macOS / Windows 真机验证；有问题向 MyGo 上游反馈 | M1 |
| 11 | **macOS 签名与公证、Windows 安装包与签名、自动更新**未验证 | 无法对外发布 | M4 前半准备证书与 Secrets，先用 `-sign -` 自签验证流程，再换正式证书 | M4 |
| 12 | **Linux 运行时依赖 GTK 3** | 部分发行版需要装包 | 文档与 `.deb`/`.rpm` 依赖声明；README 写明 | M4 |
| 13 | **OSC 7 当前目录**依赖远端 shell 配置 | “上传到当前目录”“恢复 cwd”在没配置的服务器上失效 | 提供一键“安装 OSC 7 钩子”的命令片段；缺失时回退到“询问目录” | M3 |
| 14 | **Windows OpenSSH 的 SFTP 与默认 shell** 行为差异 | 路径格式、权限、续传 | 针对 Windows 主机做专门的路径处理与测试（需真机） | M3 |
| 15 | **规格内的快捷键冲突**：规格写 ⌘⇧S 同步、⌘K 面板，定稿已改为 ⌘⌥S、⌘P | 实现跟错版本 | **以 `DESIGN.md` 为准**；规格 Page 需同步更新 | M1 |

---

## 14. 里程碑

> 工作量为**单人 + AI 辅助开发**的粗估（含测试、文档、自测），不含外部等待。每个里程碑结束都能产出一个**可试用的版本**。

### M0（开工前 2–3 天，并入 M1 计算）：骨架

仓库、`go.mod`、目录骨架、`Makefile`、`.golangci.yml`、`ci.yml`、`depguard` 规则、`theme` 包（token 抄 `DESIGN.md`）、`i18n` 骨架、空窗口跑起来，三平台构建通过。

### M1：连接 + 终端 + 网格 + 侧栏（约 3 周）

**范围**

- 主机库：增删改、标签、搜索；`hosts.json` 原子写入与迁移；钥匙串封装（`secrets`）。
- **添加 / 编辑主机**弹层（对照 `add-host.png`）：密码、密钥（OpenSSH / PEM / .ppk，含口令）、SSH Agent、键盘交互 / TOTP；多级 ProxyJump；SOCKS5 / HTTP 代理；“测试连接”并自动识别系统类型。
- `transport/ssh`：认证链、跳板、代理、`known_hosts`（首次指纹确认、变化拒绝）、keepalive。
- `session`：`Session` / `Pane`、重连状态机（退避、不可重试错误分类）、同一主机多通道复用与 `MaxSessions` 回退。
- `workspace`：块、标签（＋新建）、焦点；本机终端块；`inputbus` 先接入但同步 UI 留到 M2。
- **自适应网格**（自动模式）：1 占满、2 半、3–4 为 2×2、5–6 为 3×2，超出竖向滚动，最小块尺寸，窗口缩放重排并向远端发送 `window-change`（去抖）。
- UI：侧栏（标签分组、两行主机行、状态点）、块（块头、标签页、块脚占位）、空状态与导入 `~/.ssh/config`、断线横幅与“立即重连”、命令面板（主机搜索与基本命令）、右键菜单（主机行、块头）。
- 会话恢复（块与标签，自动布局）。
- 浅色主题。

**交付物**：三平台可运行的 `tako` 构建；Docker 集成矩阵（§11）跑通；性能基线数据（GPU、真机）。

**验收标准**

1. 对 Docker OpenSSH 矩阵中的**每种认证 + 跳板 + 代理组合**能连上并正常使用 `vim`、`top`。
2. `docker pause` 60 秒再恢复：块显示重连横幅，**回滚内容保留**，`Pane.Read` 不返回 EOF，恢复后继续可用；断线期间的键盘输入不补发。
3. 重建服务器换主机密钥：拒绝重连，并给出明确提示。
4. `~/.ssh/config` 一键导入 ≥ 12 台主机（含 `Include`、`ProxyJump`、`IdentityFile`）。
5. 9 个块空闲：CPU 0%、RSS ≤ 100 MB；冷启动 ≤ 1 s。
6. 退出再启动：恢复上次打开的块与标签并自动重连；崩溃（`kill -9`）后能恢复到 1 秒内的状态。
7. 秘密不出现在 `hosts.json`、`session.json`、日志、导出文件里（自动化检查：在所有输出里 grep 测试口令）。
8. `go test -race ./...`、`golangci-lint`、`govulncheck` 全绿；核心包覆盖率 ≥ 80%。
9. 在有显卡的 Mac 与 Windows 上跑通 §12.2 的基线并记录。

### M2：同步 + 小地图 + 吸附（约 2.5 周）

**范围**

- 同步输入接入 UI：块头“同步”chip（`DESIGN.md` 的同步样式）、`⌘⌥S` / `⌘⌥⇧S`；断线块自动跳过并显示“同步已暂停”；`StripReplies` 加固 + 模糊测试。
- **小地图**（对照定稿）：悬浮、可拖动、四角吸附；176 px、静止半透明、悬停不透明；透明度滑块；`同步 n / 6` 计数；点击切换同步、框选（⇧ 追加 / ⌥ 反选）、拖动重排；毛玻璃验证（失败则退回纯半透明）。
- **手动布局**：二叉分割树；拖分隔线调比例并吸附 1/2、1/3、2/3（⌥ 临时关闭）；边缘插入线；布局预设（横排、竖排、自动网格、自存）；双击 / `⌘⇧Enter` 放大当前块。
- 动画：约 150 ms 弹簧（布局重排、小地图落位）。
- 快捷键系统（可在设置里改，按 `DESIGN.md` §4.6）、完整右键菜单（块头、标签、主机行、小地图格子）。
- 会话恢复扩展：手动布局树、同步状态、小地图位置与透明度。

**交付物**：可用的“批量同步操作”版本；`layout` 属性测试与吸附手感说明。

**验收标准**

1. 9 个同步块里敲 `df -h`，**所有目标收到相同字节、没有互相应答**；其中一台限速到 10 KB/s，其余 8 台的延迟不受影响（总线隔离）。
2. 小地图：点击、框选（含 ⇧ / ⌥）、拖动重排、角落吸附、透明度调节全部有 `ui.Tester` 测试；静止半透明、悬停不透明可由截图对比验证。
3. 分隔线吸附到 1/2、1/3、2/3 并显示参考线；`layout` 属性测试：任意操作序列后块不重叠、面积守恒。
4. 断线块的同步自动暂停、其余不受影响；重连后自动恢复。
5. 窗口缩放、布局拖动期间终端尺寸变更去抖，远端 `top` / `vim` 正确重绘。
6. 重启后恢复手动布局与同步状态。
7. 所有快捷键与 `DESIGN.md` 的“操作→入口”表一致（脚本检查表与实现的对应）。
8. 9 块狂刷输出时帧率不低于 §12.2 预算。

### M3：监控 + 传输（约 3.5 周）

**范围**

- **监控**：三系统 `Collector`（Linux `/proc` + `df` + `nvidia-smi` / `rocm-smi`；macOS `top` / `vm_stat` / `iostat`；Windows PowerShell JSON）；`Sampler`（默认 2 s，抖动）；块脚资源条（阈值变色：CPU/内存 ≥ 80% 黄、≥ 90% 红，可配置）；点击块脚进入**监控页**（对照 `monitor.png`：概览、GPU、CPU、内存、网络、磁盘、关于；1/5/15/60 分钟窗口、暂停）；没有 GPU 的主机不显示 GPU。
- **传输**：SFTP 文件浏览（两个窗格）、上传 / 下载（含拖放与多选、文件夹）、**服务器互传**（默认经本机流式中转；可选直连 scp / rsync + 临时 agent 转发）、断点续传、失败重试、完成校验、队列（暂停 / 继续 / 取消、进度、速度、剩余时间）、同名策略与并发数设置；工具栏传输图标 + 角标。
- OSC 7 当前目录（上传默认目录；缺失时回退询问；提供安装钩子的命令片段）。
- Windows 与 macOS **主机**（被连接端）的专门测试（需真机）。

**交付物**：监控与传输完整可用；真机验证报告；性能数据（监控开销、传输吞吐）。

**验收标准**

1. Linux（含 NVIDIA GPU 主机）、macOS、Windows 三类主机的监控数值与系统自带工具（`top`、活动监视器、任务管理器）**误差 ≤ 5 个百分点**；样本解析器单元测试覆盖三平台样本。
2. 采样开销：被监控端 CPU < 1%；9 台同时采样，客户端空闲 CPU 仍接近 0%；UI 不因监控而每秒整窗重绘。
3. 2.4 GB 文件服务器互传（web-01 → db-01，经本机中转）：吞吐 ≥ 链路上限的 80%，**本机不落盘**，内存增长 < 32 MB。
4. 传输中途断网：状态变为“已暂停”，恢复后**从断点继续**，最终校验通过；目录传输同样支持。
5. 直连模式只在开启时可用；传输结束后服务器上**不残留私钥**，agent 转发关闭。
6. 同名文件的覆盖 / 跳过 / 询问三种策略都有集成测试。
7. 监控页与传输面板与 `monitor.png`、`transfer.png` 的信息结构一致（`ui.Tester` 截图对比通过）。

### M4：打磨 + 国际化 + 深色 + 打包发布（约 3 周）

**范围**

- **深色模式**（跟随系统，可强制）、终端配色方案与 iTerm2 配色导入。
- **国际化**：中 / 英完整，键对齐与伪语言测试；日期数字用系统区域格式。
- **设置**窗口（主机、终端、连接、监控、文件传输、外观、数据）与导入 / 导出（不含秘密）；Xshell、SecureCRT、Termius 导入器（含模糊测试）。
- 空状态与引导细节、动画与手感打磨、无障碍（键盘可达、屏幕阅读器标签）、GBK 编码、IME 问题收尾。
- **打包发布**：macOS 签名 + 公证 + `.dmg`；Windows NSIS 安装包 + 代码签名；Linux `.deb` / `.rpm` / PKGBUILD；签名自动更新；`release.yml` 打标签自动出草稿 Release；崩溃日志与“复制诊断信息”。
- 文档：README、用户手册、`CHANGELOG`、`DESIGN.md` 与规格 Page 同步更新。

**交付物**：`v1.0.0-rc` → `v1.0.0`（三平台签名安装包 + 自动更新）。

**验收标准**

1. 三平台安装包在**干净系统**上安装、启动、连接、升级（自动更新）全流程通过；macOS 通过 Gatekeeper，Windows 无 SmartScreen 拦截。
2. 深浅色切换无需重启；`theme` 与 `DESIGN.md` 的 token 比对测试通过。
3. 英文与中文界面完整，伪语言测试下无截断、无缺失键。
4. 导出的配置文件在**换一台电脑**导入后可用，且不含任何秘密（自动化 grep 检查）。
5. 所有性能预算达标（§12.2）；`goleak`、`-race` 全绿。
6. 手测清单（连接、同步、重排、传输、监控、重连、会话恢复）在 macOS 与 Windows 真机上全部通过。
7. Linux 在 Ubuntu 22.04/24.04 与 Fedora 最新版上启动并通过冒烟清单。

### 工作量汇总

| 里程碑 | 粗估 | 累计 | 主要不确定性 |
|---|---|---|---|
| M1 | 3 周（含骨架 0.5 周） | 3 周 | 真机差异、IME、MyGo API 磨合 |
| M2 | 2.5 周 | 5.5 周 | 吸附手感、毛玻璃、手动布局树的复杂度 |
| M3 | 3.5 周 | 9 周 | Windows / macOS 主机差异、OSC 7、直连传输 |
| M4 | 3 周 | 12 周 | 证书申请与公证、GBK、无障碍 |
| 缓冲 | +2 周 | 约 14 周 | MyGo 上游问题、真机排查 |

---

## 15. 需要你决定 / 确认的事

1. **仓库**：名称、归属账号，公开还是私有？需要先把 GitHub 连上，我才能直接在仓库里开发。（模块路径暂定 `github.com/wei-dong/tako`）
2. **架构与依赖注入**：我按“扁平 + 手写构造函数注入”定了默认；如果你想要别的（比如 DI 库），现在改成本最低。
3. **发布与签名**：
   - macOS：是否有 Apple Developer 账号（Developer ID 证书 + 公证）？没有的话，M4 先发自签版，用户首次打开要手动放行。
   - Windows：是否购买代码签名证书（或用云签名服务）？不签名会触发 SmartScreen 提醒。
4. **真机验证**：你有没有真实的 Windows（开 OpenSSH Server）和 macOS（开启远程登录）机器，可以让我连上做监控与传输验证？另外 M1 起需要 macOS / Windows 真机跑冒烟（注：效果图里的 `win-build`、`mac-mini` 只是示例主机名）。
5. **同步输入的密码保护**：检测到“无回显”（密码提示）时是否自动暂停该次输入的同步？（不属于“危险命令确认”，是为防止 `sudo` 密码被发到所有机器。）
6. **回滚行数**：默认 10 000 行、每终端 ≤ 4 MB 是否可以？
7. **GBK 等非 UTF-8 编码**：初版是否必须？（放在 M4，若不需要可砍。）
8. **Linux 发行版范围**：除 Ubuntu / Fedora 外是否还有必须支持的？（运行需要 GTK 3。）
9. **自动更新**：用 GitHub Releases 作为更新源是否可以？还是你有自己的服务器？
10. **规格 Page 更新**：定稿的快捷键（⌘⌥S 等）、小地图尺寸、去冗余决定与旧规格有出入，是否让我把规格 Page 同步更新成最终版？
