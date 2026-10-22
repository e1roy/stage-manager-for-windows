<p align="center">
  <img src="stage_mgr_go/resources/icon.png" width="128" height="128" alt="Stage Manager for Windows 图标">
</p>

<h1 align="center">Stage Manager for Windows</h1>

<p align="center">
  把 macOS 的台前调度带到 Windows 10 / 11：当前应用留在舞台中央，其他应用按应用分组收进屏幕左侧的侧边栏。
</p>

<p align="center">
  <strong>简体中文</strong> · <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/e1roy/stage_manager_for_windows/releases"><img src="https://img.shields.io/github/v/release/e1roy/stage_manager_for_windows" alt="最新版本"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/e1roy/stage_manager_for_windows" alt="MIT 许可证"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%2F11-0078D4" alt="Windows 10/11">
</p>

## 项目简介

Stage Manager for Windows 是一个 Windows 上的窗口管理器。你正在用的应用留在桌面中央的“舞台”上，其他应用按应用分组、收进屏幕左侧的侧边栏。点击侧边栏里的卡片，当前应用飞进那张卡片的位置，被点的应用则从卡片里飞回原处，侧边栏其余卡片保持不动。

**每台显示器都有自己的舞台和侧边栏。** 窗口按它所在的显示器归属，把窗口拖到另一台显示器时，它会加入那台显示器的舞台，而不会被当成游离窗口收走。

两种侧边栏模式：

- **固定模式：** 用 Windows AppBar API 为侧边栏保留屏幕边缘，最大化的窗口不会遮住它；出现全屏应用时自动让出置顶。
- **浮动模式：** 有空间时显示，被舞台上的窗口遮住时滑走；把指针移到显示器最左侧 2 像素即可唤出（两台显示器相接的内侧边缘需停留约 250 毫秒，避免跨屏时误触）。

## 下载安装

请从 [GitHub Releases](https://github.com/e1roy/stage_manager_for_windows/releases/latest) 下载最新版安装程序。安装包为当前版本构建产物，仅为当前用户安装，不需要管理员权限，也不需要额外安装任何运行时。

> [!WARNING]
> 安装包尚未进行代码签名，Windows SmartScreen 可能显示“未知发布者”提示。运行前请核对 Release 页面提供的 SHA-256。

## 主要功能

- 只有当前应用留在舞台，其他应用收进所在显示器的侧边栏（可关闭，关闭后侧边栏只列出它们）
- 点击卡片切换应用；Alt+Tab、任务栏点击和新打开的窗口会立即成为该显示器的舞台
- 把卡片拖到桌面、或按住 Shift 点击，可让该应用加入当前舞台，与已有应用并排
- 一个舞台可以放多个应用：它们作为一组离开舞台、合并成一张卡片，点击卡片或激活其中任一应用时整组一起回来；右键可拆开组合
- 把窗口拖进侧边栏，可将该应用单独移出舞台
- 点击桌面壁纸，把该显示器的舞台收进侧边栏（可关闭）
- 手动最小化的窗口交给任务栏：它离开舞台，也不在侧边栏出现，从任务栏还原后重新成为舞台
- 同一应用的多个窗口叠成一张卡片（最多显示 3 层）；应用变多时卡片自动缩小，再多则可滚轮滚动
- 卡片带 3D 透视倾斜、阴影和大号应用图标，指针悬停时转向你
- 不在舞台上的窗口被移到桌面之外“停放”，保持大小和状态，回来时立刻恢复
- 退出、崩溃或注销时，所有停放的窗口都会放回桌面（崩溃靠日志恢复）
- 托盘图标：左键打开设置，右键菜单可显示/隐藏侧边栏、暂停、退出
- 设置：显示方式、宽度、图标大小、立体透视、隐藏舞台之外的窗口、点击桌面清空舞台、按显示器开关侧边栏、开机启动、暂停、找回屏幕外的窗口
- `StageManagerForWindows.exe --quit` 让正在运行的实例退出并放回所有窗口
- 不收集任何数据，不联网

## 系统要求

- Windows 10 / Windows 11
- x64 处理器
- 桌面窗口管理器（DWM）已启用

以管理员身份运行的应用，非管理员的 Stage Manager for Windows 可能无法移动或激活它们。

## 使用方法

1. 安装并启动 Stage Manager for Windows。
2. 在托盘图标上右键打开 **设置…**：选择固定或浮动模式、是否隐藏舞台之外的窗口、侧边栏宽度和图标大小。
3. 点击卡片，让该应用登上舞台；当前应用会收进侧边栏。
4. 把卡片拖到桌面上（或按住 Shift 点击），让该应用与当前应用同时显示；之后它们会作为一组进出舞台。
5. 点击桌面壁纸清空舞台；打开或激活任意应用即可开始新的舞台。
6. 浮动模式下侧边栏被遮挡时，把指针移动到显示器最左侧 2 像素区域唤出它。

其他操作：

| 操作 | 控制方式 |
| --- | --- |
| 打开设置 | 左键点击托盘图标，或右键菜单 →“设置…” |
| 显示 / 隐藏侧边栏 | 右键菜单 →“显示 / 隐藏侧边栏”（仅浮动模式） |
| 浏览应用卡片 | 在侧边栏上滚动鼠标滚轮 |
| 把应用加入舞台 | 把卡片拖到桌面上，或按住 Shift 点击卡片 |
| 把应用移出舞台 | 把它的窗口拖进侧边栏 |
| 拆开组合 / 关闭窗口 | 右键卡片 →“拆开组合” / “关闭窗口” |

设置保存在 `%LOCALAPPDATA%\Stage Manager\settings.json`，停放日志保存在同一目录。

## 实现原理

Stage Manager for Windows 用 Go 写成，界面由 [MyGo](https://github.com/egoist/mygo) 原生 UI（Direct3D 11）绘制，纯 Go、无 cgo、无 WebView2。

| 组件 | 作用 |
| --- | --- |
| MyGo 原生 UI | 侧边栏、设置界面、飞行层与静帧绘制（3D 透视、阴影、圆角） |
| Win32 / DWM | 窗口枚举与过滤、停放与还原、AppBar、WinEvent 钩子、`PrintWindow` 静帧 |
| 窗口停放 | 把窗口移到桌面之外，保持大小与所在显示器的 DPI，附带崩溃恢复日志 |
| 飞行层 | 每台显示器一层透明、点击穿透的窗口，静帧在卡片与舞台之间飞行，真实窗口在其下方交接 |
| WinEvent 钩子 | 不轮询即可响应激活、新窗口、移动、最小化和关闭 |
| 看门狗 | 主线程 5 秒无响应时，按日志把所有停放窗口放回，并写出各 goroutine 的堆栈 |

代码结构见 [`stage_mgr_go/README.md`](stage_mgr_go/README.md)。

## 从源码构建

需要 Go 1.27.1 或更高版本：

```powershell
git clone git@github.com:e1roy/stage_manager_for_windows.git
cd stage_manager_for_windows\stage_mgr_go
go run .              # 开发运行
go test ./...         # 单元测试与无头界面测试
go tool mygo vet .    # 检查 UI 生命周期问题
go tool mygo build    # 生成 build\windows-amd64\StageManagerForWindows.exe 和安装包
```

开发时可用的环境变量，避免调试时打乱自己的窗口：

| 变量 | 作用 |
| --- | --- |
| `STAGEMANAGER_APP_FILTER` | 只管理这些可执行文件（逗号分隔，如 `notepad,mspaint`） |
| `STAGEMANAGER_DATA_DIR` | 设置和停放日志的目录（默认 `%LOCALAPPDATA%\Stage Manager`） |
| `STAGEMANAGER_DEBUG` | 输出决策日志；此时第二个实例传 `--debug-hang` 可故意卡住主线程，用来检验看门狗 |
| `STAGEMANAGER_SNAPSHOT_DIR` | 让 `go test ./internal/app -run TestSnapshots` 把界面渲染成 PNG |

## 当前限制

- 侧边栏里显示的是窗口离开舞台时的静帧：Windows 不绘制桌面之外的窗口，停放中的窗口本来也不会变化。
- 用 `PrintWindow` 拍不到内容的窗口（部分游戏、受 DRM 保护的视频）显示为带图标的磨砂卡片。
- 以管理员身份运行的应用，非管理员的 Stage Manager for Windows 可能无法移动或激活它们。
- 置顶窗口不参与舞台，浮在最上层。

## 参与贡献

欢迎提交 Issue 和 Pull Request。修改代码时建议：

1. Fork 仓库并创建目标明确的分支。
2. 所有状态只在 MyGo 主线程上读写；耗时的截图等放在 goroutine，再回到主线程。
3. 运行 `go test ./...` 与 `go tool mygo vet .`，并实测固定和浮动两种模式。
4. 在 Pull Request 中说明行为变化和手动验证结果。

Bug 反馈和功能建议请提交到 [GitHub Issues](https://github.com/e1roy/stage_manager_for_windows/issues)。

## 隐私说明

Stage Manager for Windows 仅在本地处理窗口句柄、标题、图标和窗口画面，不包含分析统计、遥测、账户系统或网络服务。

## 开源许可证

Stage Manager for Windows 使用 [MIT License](LICENSE) 开源。
