# Stage Manager for Windows

在 Windows 10/11 上实现 macOS“台前调度”：你正在用的应用留在中间的舞台上，其他应用按应用分组，收进屏幕左侧的侧边栏。**每台显示器都有自己的舞台和侧边栏。**

用 [MyGo](https://github.com/egoist/mygo) 写成，纯 Go、无 cgo、无 WebView2。界面由 MyGo 原生 UI（Direct3D 11）绘制，与系统交互的部分直接调用 Win32。

## 它是怎么工作的

- **每台显示器一个舞台。** 窗口按它所在的显示器归属，停放时也不会换到别的显示器，因此 DPI 不会变。
- **卡片显示的是静帧。** 侧边栏里画的是窗口离开舞台时拍下的画面，由 MyGo 直接绘制，带真正的 3D 透视、阴影和圆角，图标与标题自然叠在上面。
- **切换靠飞行层。** 每台显示器有一层透明、点击穿透的窗口；静帧在卡片与舞台之间飞行，真实窗口在飞行层下方交接。
- **不在舞台上的窗口被“停放”。** 移到桌面之外而不是最小化，所以它们保持大小和状态，回来时立刻恢复；每次停放都会写日志，崩溃后靠日志放回。

## 功能

- 只有当前应用留在桌面，其他应用收进所在显示器的侧边栏（可关闭，关闭后侧边栏只列出它们）
- 手动最小化的窗口交给任务栏：它离开舞台，也不在侧边栏出现，从任务栏还原后重新成为舞台
- 点击卡片：当前应用飞进侧边栏、占据被点卡片的位置；被点的应用从卡片里飞回原处
- Alt+Tab、任务栏、新打开的窗口会立即成为该显示器的舞台
- 把卡片拖到桌面：该应用加入当前舞台，与已有应用并排
- 右键卡片：切换到舞台 / 加入当前舞台 / 拆开组合 / 关闭窗口
- 点击壁纸：清空该显示器的舞台（可关闭）
- 把窗口拖到另一台显示器：它加入那台显示器的舞台，而不会被当成“游离窗口”收走
- 同一应用的窗口叠成一张卡（最多显示 3 层），应用多时卡片自动缩小，再多则可滚轮滚动
- 固定模式：用 AppBar 为侧边栏保留屏幕边缘，全屏应用出现时自动让出置顶
- 浮动模式：有空间时显示，被舞台窗口遮住时滑走；指针移到左边缘唤出
- 停放的窗口保持大小与状态，退出、崩溃、注销时全部放回（崩溃靠日志恢复）
- 托盘图标：左键打开设置，右键菜单（显示/隐藏、设置、暂停、退出）
- `Stage Manager for Windows.exe --quit` 让正在运行的实例退出并放回所有窗口（安装/卸载时使用）
- 不收集任何数据，不联网

## 架构

```
main.go                     入口：单实例、--quit、启动 app
internal/
  winapi/    Win32 薄封装（syscall，无状态）：窗口、显示器、WinEvent 钩子、
             子类化、AppBar、PrintWindow 静帧、图标、版本信息
  catalog/   枚举并过滤应用窗口，识别所属应用（含 UWP 宿主），应用名
  parking/   把窗口停到桌面之外 / 放回，带崩溃恢复日志
  stage/     纯逻辑：每台显示器的舞台与侧边栏顺序（有单元测试）
  settings/  设置的读取、归一化、原子保存（有单元测试）
  app/       Stage Manager for Windows 本体，全部在 MyGo 主线程上运行
    app.go          显示器同步、刷新、初始舞台、游离/逃逸窗口处理
    events.go       WinEvent 处理、前台跟随、点击壁纸、指针轮询
    switch.go       舞台切换与真实窗口的交接
    flights.go      飞行层（每台显示器一个）
    strip.go        侧边栏窗口：放置、AppBar、浮动显示/隐藏、点击穿透
    stripview.go    侧边栏界面（卡片、透视、悬停、拖动、右键菜单）
    layout.go       纯几何：卡片位置、窗口叠放、图标位置（有单元测试）
    media.go        静帧与图标缓存
    settingsview.go 设置窗口
    tray.go         托盘图标与托盘菜单
```

**线程模型。** 所有状态只在 MyGo 主线程上读写：WinEvent 钩子在主线程安装、在主线程回调；定时器、截图 goroutine 的结果都通过 `mygo.RunOnMain` 回到主线程。截图（`PrintWindow` 会等目标应用自己绘制）只在 goroutine 里做，并有超时，卡死的应用不会拖住切换。

**看门狗。** 一个后台 goroutine 每 2 秒确认主线程是否响应。若 5 秒无响应，它直接按磁盘上的停放日志把所有停放的窗口放回桌面（不经过主线程），并把所有 goroutine 的堆栈写到数据目录的 `hang-*.log`；主线程恢复后，Stage Manager for Windows 会按当前舞台重新收起该收的窗口。

**坐标。** 与系统打交道一律用物理像素（进程是 Per-Monitor V2）；侧边栏和飞行层窗口由 Win32 摆放，MyGo 只负责在窗口内按 DIP 绘制。

**侧边栏窗口**是 MyGo 的 flyout（挂在托盘图标上）：无重定向位图、经 DirectComposition 带透明度呈现、置顶、不激活。窗口始终是 layered 的，指针不在卡片上时加上 `WS_EX_TRANSPARENT`，点击就穿透到下面的窗口。

## 构建与运行

需要 Go 1.27.1 以上（`GOTOOLCHAIN=auto` 时会自动下载）。

```powershell
cd stage_mgr_go
go run .                      # 开发运行
go tool mygo dev              # 改代码自动重编译重启
go test ./...                 # 单元测试与无头界面测试
go tool mygo vet .            # 检查 UI 生命周期问题
go tool mygo build            # build/windows-amd64/Stage Manager for Windows.exe 和安装包
```

开发时可用的环境变量：

| 变量 | 作用 |
| --- | --- |
| `STAGEMANAGER_APP_FILTER` | 只管理这些可执行文件（逗号分隔，如 `notepad,mspaint`），测试时不会动到你自己的窗口 |
| `STAGEMANAGER_DATA_DIR` | 设置和停放日志的目录（默认 `%LOCALAPPDATA%\Stage Manager`） |
| `STAGEMANAGER_DEBUG` | 输出决策日志；此时第二个实例传 `--debug-hang` 可故意卡住主线程 12 秒，用来检验看门狗 |
| `STAGEMANAGER_SNAPSHOT_DIR` | 让 `go test ./internal/app -run TestSnapshots` 把侧边栏和设置界面渲染成 PNG |

## 已知限制

- 侧边栏里显示的是窗口离开舞台时的静帧：Windows 不绘制桌面之外的窗口，停放中的窗口本来也不会变化。
- 用 `PrintWindow` 拍不到内容的窗口（部分游戏、受 DRM 保护的视频）显示为带图标的磨砂卡片。
- 以管理员身份运行的应用，非管理员的 Stage Manager for Windows 可能无法移动或激活它们。
- 置顶窗口不参与舞台，浮在最上层。
