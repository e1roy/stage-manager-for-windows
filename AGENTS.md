# AGENTS.md

给接手这个仓库的 agent 的上手说明。先读完这一页，再动代码。

## 一句话

Stage Manager for Windows：在 Windows 10/11 上复刻 macOS“台前调度”。当前应用留在舞台（桌面中央），其他应用按应用分组收进左侧侧边栏；**每台显示器各有一个舞台和侧边栏**。

## 先搞清楚哪些代码是活的

| 路径 | 状态 |
| --- | --- |
| `stage_mgr_go/` | **唯一在维护的实现**。纯 Go + [MyGo](https://github.com/egoist/mygo)，无 cgo、无 WebView。 |
| `StageManager/`、`installer/`、`.github/workflows/`、`start-stagebar.bat` | 旧的 C# WPF 版，已从工作区删除（git 中为 staged deletion）。不要复活，也不要照着它改。 |
| `README.md`、`README.zh-CN.md`（根目录） | 部分内容仍在描述 C# 版（.NET 构建、Inno Setup、`%LOCALAPPDATA%\StageManager`）。**构建与架构以 `stage_mgr_go/README.md` 为准。** |
| `其他项目参考代码/` | 第三方参考项目（StageManagerForWindows，C#），只读参考，**未纳入版本控制，不要 `git add`**。 |
| `.codebuddy/plans/` | 之前做过的问题分析（如阴影跳变），排查同类问题前值得一读。 |
| `start-stage-manager.bat` | 本地一键：先 `--quit` 旧实例，再编译并启动 `build/windows-amd64/StageManagerForWindows.exe`。 |

## 常用命令

都在 `stage_mgr_go/` 下执行（需要 Go 1.27.3+，`GOTOOLCHAIN=auto` 会自动下载）：

```powershell
go build ./...                # 编译
go vet ./...
go test ./...                 # 单元测试 + 无头界面测试，几秒内跑完
go tool mygo vet .            # MyGo 的 UI 生命周期检查，改 UI 后必须跑
go run .                      # 开发运行
go tool mygo dev              # 改代码自动重编译重启
go tool mygo build            # 产出 build/windows-amd64/StageManagerForWindows.exe 和安装包
```

提交前至少保证 `go build ./... && go vet ./... && go test ./... && go tool mygo vet .` 全部通过。

## 安全地运行它（重要）

Stage Manager for Windows 会把**真实窗口**移到桌面外（“停放”）。开发调试时务必隔离：

```powershell
$env:STAGEMANAGER_APP_FILTER = "notepad,mspaint"   # 只管理这些 exe，不碰你自己的窗口
$env:STAGEMANAGER_DATA_DIR   = "$env:TEMP\stagebar-dev"  # 独立的设置和停放日志
$env:STAGEMANAGER_DEBUG      = "1"                 # 打印决策日志（debugf）
go run .
```

- 结束实例用 `StageManagerForWindows.exe --quit` 或 Ctrl+C，这样会把所有停放的窗口放回。**不要直接 kill 进程**；万一 kill 了，下次启动会按 `parked-windows.json` 日志恢复。
- `STAGEMANAGER_DEBUG` 开启时，第二个实例传 `--debug-hang` 会故意卡住主线程 12 秒，用来验证看门狗。
- `STAGEMANAGER_SNAPSHOT_DIR=<目录> go test ./internal/app -run TestSnapshots` 把侧边栏和设置窗口渲染成 PNG，不用启动应用就能看界面效果。
- 默认数据目录：`%LOCALAPPDATA%\Stage Manager`（`settings.json`、`parked-windows.json`、看门狗写的 `hang-*.log`）。

## 代码地图

```
stage_mgr_go/
  main.go            入口：单实例锁、--quit / --debug-hang、Ctrl+C、数据目录
  mygo.json          应用名、版本、图标、输出目录
  internal/
    winapi/    Win32 薄封装（syscall），无状态。新的系统调用加在这里；通用 DLL/proc 在 procs.go，专用的（COM、version.dll、DWM 缩略图）就近声明在各自文件里
    catalog/   枚举/过滤应用窗口，识别所属应用（含 UWP 宿主），应用显示名；STAGEMANAGER_APP_FILTER 在这里生效
    parking/   停放/放回窗口 + 崩溃恢复日志
    stage/     纯逻辑：每台显示器的舞台、侧边栏分组与顺序（不认识窗口，有单测）
    settings/  设置读取、归一化、原子保存（有单测）
    i18n/      界面文字：简体中文、繁体中文、英文、日文各一张 Strings 表，FromLANGID 把 Windows 显示语言映射过来（有单测）
    app/       Stage Manager for Windows 本体，全部运行在 MyGo 主线程
      app.go          App/display/group/set 结构、显示器同步、refresh、初始舞台、游离/逃逸窗口
      events.go       WinEvent 处理、前台跟随、点击壁纸、指针轮询、Win+Tab 打开概览
      drag.go         拖动窗口：拖到另一台显示器时那边的舞台让位、松手、拖进侧边栏、键盘移动窗口
      carddrag.go     拖动卡片：松手去向（aim：回原位/侧边栏内排序/加入舞台/搬到另一台显示器的侧边栏）、让位布局、moveSet/reorder
      switch.go       舞台切换、真实窗口交接（handover）、切换排队/打断、卡片动作
      flights.go      每台显示器一层的飞行层：静帧在卡片和舞台间飞行、阴影交接
      pacer.go        按 DWM 合成节奏（DwmFlush）要帧，飞行层和概览用它代替 AnimationFrame
      strip.go        侧边栏窗口：摆放、AppBar、浮动显示/隐藏、点击穿透
      stripview.go    侧边栏界面：卡片、透视、悬停、拖动、右键菜单
      layout.go       纯几何：卡片布局、窗口叠放、图标位置（有单测）
      media.go        静帧（PrintWindow）与图标缓存、内存预算
      iconfit.go      裁掉图标四周透明边
      peek.go         任务栏缩略图 peek 时临时放回被停放的窗口
      overview.go     “所有窗口”概览（类 Mission Control）：侧边栏底部按钮打开，铺开本显示器所有窗口，点选切换舞台
      corner.go       左下角触发角：Win11 任务栏居中且关闭小组件时，在显示器左下角盖一个不抢焦点的小窗口，靠近时显示标记，点击打开概览
      settingsview.go 设置窗口；tray.go 托盘；debug.go 调试日志、看门狗、帧统计与主线程慢任务计时
```

## 核心流程：一次舞台切换

1. 触发：点卡片 → `stripview` 调 `a.request(actPick…)`；Alt+Tab/任务栏 → `events.onWinEvent` → `onForeground` → `settleForeground`；点壁纸 → `switchStage(d, nil, …)`。
2. `switch.go: switchStage` 算出 departing / arriving 窗口。若已有切换在跑（`a.busy`），新请求放进 `a.queued` 并 `flights.interrupt`，空中的窗口原地掉头，不等待。
3. `media.capture` 在 goroutine 里给离场窗口拍静帧（带超时，卡死的应用不会拖住切换），完成后回主线程。
4. 更新模型 `d.model.Switch` → `arrange` 重建 `d.sets` / `d.onStage` → `flights.run` 开始飞行。
5. `handover` 的回调在飞行中交接真实窗口：`takeOff`（停放离场窗口、激活到场窗口）→ `land`（放回到场窗口）→ `stop`/`done`（收尾，`runQueued` 执行排队的切换）。

另外每 1.5s 有一次兜底 `refresh`，事件驱动的 `refreshSoon` 负责即时响应。

## 必须遵守的约束

- **线程模型**：`App` 的所有状态只在 MyGo 主线程读写。后台 goroutine 的结果必须用 `post(fn)` / `after(d, fn)`（内部是 `mygo.RunOnMain`）回到主线程。WinEvent 钩子在主线程安装和回调。唯一例外是 Win+Tab 的低级键盘钩子（`winapi/keyboard.go`）：它在自己的线程里跑消息循环，免得主线程一忙全系统打字卡顿，回调里只 `post`。不要加锁来“修”并发，正确做法是回到主线程。
- **不要在主线程做可能阻塞的事**：`PrintWindow` 会等目标应用自己绘制，只能在 goroutine 里做并带超时。看门狗在主线程 5 秒无响应时会直接按日志放回所有窗口。
- **坐标**：与系统打交道一律是物理像素（进程是 Per-Monitor V2，`winapi.Rect`）；MyGo 在窗口内按 DIP 绘制（`ui.Rect`）。两者之间的换算集中在 `strip.toScreen`、`display.mon.Scale()` 一类地方，别在别处随手乘 DPI。
- **停放而不是最小化**：隐藏窗口一律走 `parking.Service`（移到同一显示器旁的桌面外，保持 DPI），每次变动写日志。绕过它直接 `SetWindowPos` 移走窗口，崩溃后窗口就回不来了。用户手动最小化的窗口交给任务栏，不进侧边栏。
- **分层**：`winapi` 不持有应用状态；`stage` 不认识 HWND（规则改动先在 `stage` 里写测试）；纯几何放 `layout.go`。新逻辑放进对应层，别把 Win32 调用塞进 `stage`/`settings`。
- **依赖**：除 MyGo 外不引入第三方依赖，不用 cgo。Windows 专用文件加 `//go:build windows`。
- **设置**：新增设置项要同时改 `Settings`、`Default()`、`Normalize()`（如有范围）、`Equal/Clone`（如是引用类型）、`settingsview.go` 和 `applySettings`。
- **界面文字**：用户看得到的字一律走 `a.tr()`（`i18n.Strings`），不要在代码里写死中文。新增文字要在 `i18n.Strings` 加字段并补齐四种语言，`TestComplete` 会检查缺失和格式动词是否一致。设置里语言为空表示跟随 Windows 显示语言。

## 代码风格

- 注释用英文完整句子，说明“为什么”和与 macOS/Windows 行为的对应关系，而不是复述代码。参考 `switch.go`、`flights.go` 的写法。
- 命名短而具体（`park`、`fg`、`air`、`landing`），常量旁注明取值理由（如 `takeOffDelay`）。
- 文档（README、本文件）用中文。

## 测试

- 有单测的：`stage`、`settings`、`i18n`、`app/layout`、`app/stripview`（无头渲染，通过 `App.act` 截获卡片动作）、`app/iconfit`、`app/peek`、`winapi/monitor`。
- `catalog`、`parking`、飞行动画、窗口交接**没有自动化测试**，改动后需要手动验证：
  - 固定模式和浮动模式各切换几次，包括快速连点打断正在进行的切换；
  - 多显示器（不同 DPI）下切换、把窗口拖到另一台显示器、把窗口拖进侧边栏；
  - 拖卡片到桌面 / Shift+点击合并舞台，右键拆开组合；
  - `--quit` 和强杀后重启，确认所有窗口都回到桌面。
- 动画类问题（闪烁、阴影跳变）靠录屏逐帧看，参考 `.codebuddy/plans/阴影嘭跳变分析_*.md` 的方法。

## 当前进行中 / 已知问题

- 飞行起飞与落地时窗口阴影“嘭”一下跳变：飞行代理的假阴影与真实 DWM 阴影在交接帧叠加。相关代码在 `flights.go`（`takeOffDelay`、`landingDelay`、`ownShadow`、`shade`）和 `switch.go` 的 `handover`。最近一次提交 `wip: checkpoint before shadow handover fixes` 就是为此做的检查点。
- 侧边栏显示的是离场时的静帧（Windows 不绘制桌面外的窗口）；`PrintWindow` 拍不到内容的窗口显示为带图标的磨砂卡片。
- 以管理员权限运行的窗口，非管理员的 Stage Manager for Windows 可能无法移动或激活。
- 拖动窗口时，飞行层不能压到被拖窗口下面：那样系统很少把飞行层的画面送上屏幕，约 700ms 只有 30–40 帧。现在飞行层留在最上层，每帧避开被拖窗口所在的区域（`flights.go` 的 `hole`），看起来像从它下面飞过。`STAGEMANAGER_DEBUG` 下每次飞行结束记一行 `flights: ...: N frames in ..., longest gap ...`，排查卡顿先看这一行。
- 动画卡顿的排查（`STAGEMANAGER_DEBUG`，`start-stage-manager.bat` 还会打开 `MYGO_FRAME_STATS=1`）：
  - `flights:` / `strip ...:` / `flight layer ...:` / `overview ...:` 行给出帧数、最长间隔（及当时进度或状态）、最长构建时间；`main thread: X took T` 列出占住主线程超过 8ms 的任务；`... took T: a 1ms, b 2ms` 是切换准备阶段的分步计时；`mygo: frame ... took` 是 MyGo 单帧的构建/绘制/提交耗时。
  - MyGo 对动画窗口不限帧率，整屏飞行层曾每秒画约 400 帧、把 GPU 占满。整屏动画一律用 `pacer` 按合成节奏要帧，不要再用 `AnimationFrame`。
  - 显示整屏的飞行层要 10–15ms（首次约 50ms），隐藏也要约 10ms：飞行层空了以后留 2 秒（`layerLinger`），启动时、拖卡片开始时提前显示（`standBy`），别在动画进行中显示。
  - 拖卡片时别让其他卡片响应悬停（会按原尺寸截图）；侧边栏内排序时卡片留在侧边栏窗口里画，只有拖出去（加入舞台、去另一台显示器）才交给飞行层。
  - 高分屏上原尺寸截图很大，设置里的“高分屏上减轻动画负担”（`LightFlights`，默认开）按 100% 缩放的清晰度截飞行用的图。
- 拍静帧（`PrintWindow` + `PW_RENDERFULLCONTENT`）会让部分应用闪一下，已确认的有 JetBrains IDE。IDEA 打开“查找引用”等弹窗时，前台会先切到弹窗（`SunAwtWindow`），约 10ms 后又切回主窗口（`SunAwtFrame`）。`events.go: onForeground` 原来会把主窗口当成“刚离开的窗口”去拍静帧，导致每次打开弹窗都闪。现在焦点只在同一进程内移动时不拍（`sameProcess`）。以后新增拍照时机时，不要在用户正在用的、舞台上的窗口上随便拍；`STAGEMANAGER_DEBUG` 下每次拍照都会记一行 `capture [...]`，可以用来排查。

## Git

- 主分支 `main`，提交信息用 `feat:` / `fix:` / `chore:` / `wip:` 前缀。
- 不要提交 `stage_mgr_go/build/`、`*.exe`、`其他项目参考代码/`、`.codebuddy/`（已在 `.gitignore`）。`.claude/skills/` 是团队共用的 Claude skill，随仓库提交。
- `.bat` 文件保持 CRLF（见 `.gitattributes`），且 bat 里有中文，开头需要 `chcp 65001`。

## 打包
打包时候, 如无明确说明, 版本号增加`0.0.1`, 增加一个最小的版本
