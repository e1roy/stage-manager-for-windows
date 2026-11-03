# 代码地图

给要改这个仓库的人（和 agent）看的导航。分工是这样：

- 根目录 `AGENTS.md`：不能违反的规则、常用命令、当前已知问题。
- 本文件：某个行为在哪、改一处要跟着改哪些、调试和测试从哪下手。
- `stage_mgr_go/README.md`：产品是什么、怎么构建、怎么安全地跑起来。

三份都只描述 `stage_mgr_go/`。仓库里其他文件（`start-stage-manager.bat`、`docs/screenshot.png`）是辅助。

## 分层与依赖

```
main.go                     MyGo 生命周期、单实例、--quit / --debug-hang、数据目录
  │
internal/app                应用本体，全部状态只在 MyGo 主线程
  ├─ internal/stage         纯规则：每台显示器上哪些应用在舞台、侧边栏怎么分组排序（不认识 HWND）
  ├─ internal/settings      设置的读取、归一化、原子保存
  ├─ internal/i18n          四种语言的文字表
  ├─ internal/catalog       枚举窗口 → 归到应用（含 UWP 宿主）、应用名
  ├─ internal/parking       停放 / 放回窗口 + 崩溃恢复日志
  ├─ internal/update        查 GitHub 最新 release、比较版本、解析系统代理（只读，不下载安装）
  └─ internal/winapi        Win32 薄封装（syscall）：窗口、显示器、钩子、AppBar、静帧、DWM
```

依赖只能从上往下：

| 层 | 可以做 | 不可以做 |
| --- | --- | --- |
| `winapi` | 直接调 Win32、持有句柄 | 持有应用状态、知道舞台或卡片 |
| `catalog` / `parking` | 用 `winapi`，认识窗口 | 知道舞台、卡片、设置界面 |
| `stage` / `settings` / `i18n` / `update` | 纯逻辑，可单测 | 碰 `winapi`、碰 `HWND` |
| `app` | 组合上面全部，跑 MyGo | 把 Win32 细节散落到 UI 绘制里 |

除 MyGo 和 `golang.org/x/image` 外不引入第三方依赖，不用 cgo。Windows 专用文件带 `//go:build windows`。

## 目录

```
stage_mgr_go/
  main.go                入口；单实例锁；Ctrl+C；--quit / --debug-hang
  mygo.json              应用名、identifier、版本、图标、输出目录（build/）
  resources/             icon.png（托盘、窗口、安装包共用）
  internal/
    app/                 见下表
    winapi/              Win32 薄封装
    catalog/             窗口枚举与归属
    parking/             停放与恢复
    stage/               舞台规则（有测）
    settings/            设置（有测）
    i18n/                界面文字（有测）
    update/              读 `.version` 得到最新版本、版本比较、系统代理解析（有测）
```

## internal/app 逐文件

| 文件 | 关键符号 | 负责什么 | 改动时注意 |
| --- | --- | --- | --- |
| `app.go` | `App`、`display`、`group`、`set`、`Start`、`syncDisplays`、`refresh`、`initStage`、`applySettings`、`shutdown`、`post`/`after` | 状态中枢：显示器同步、窗口刷新、初始舞台、空闲补位、`settings` 应用 | 新状态字段加在 `App` / `display` 上，只在主线程读写；后台结果一律走 `post`/`after` |
| `events.go` | `hookEvents`、`onWinEvent`、`onForeground`、`settleForeground`、`desktopClick`、`startPolling`、`poll`、`syncWinTab`/`winTab` | WinEvent 驱动：前台跟随、点壁纸清舞台、指针轮询（浮出侧边栏、peek、触发角）、Win+Tab 概览 | 新增拍照时机要避免拍用户正在用的窗口（JetBrains 弹窗闪一下）；调试日志有 `capture [...]` |
| `switch.go` | `switchStage`、`handover`（`takeOff` / `land` / `stop` / `done`）、`switchRequest`、`runQueued`、`ended`、`arrange`（同步 `model.Keep(AllStages)`、标出台前的 `set.staged`）、`reveal`（`AllStages` 时离场的卡片回自己的槽位，可能在可视区外，飞行前先滚到它）、`action` 常量（`actPick`…`actFront`/`actUnstage`）、`request`、`requestOverview`、`pick`/`join`/`closeApps`/`toggleCompanion` | 舞台切换、真实窗口交接、切换排队与打断、卡片与舞台窗口列表的动作分发 | 动画时长常量在 `flights.go`；改交接顺序先看 `handover` 的注释 |
| `flights.go` | `flights`、`layer`、`flight`、`trip`、`badge`、`pose`、`shade`、`ownShadow`、`around`、`flightTime`/`takeOffDelay`/`landingDelay` | 每台显示器一层的飞行层：静帧在卡片与舞台间飞行、阴影交接、拖动卡片时的让位 | 整屏动画必须走 `pacer`；飞行层要避开被拖窗口（`hole`）；一处时序改动会牵动阴影 |
| `pacer.go` | `pacer.frame` | 用 `DwmFlush` 按 DWM 合成节奏要帧，代替 `AnimationFrame` | 只有整屏窗口（飞行层、概览）用它，小窗口别用 |
| `strip.go` | `strip`、`place`、`applyBounds`、`message`、`poll`、`approach`、`setShown`、`setClickThrough`、`holdOpen`、`line` | 侧边栏窗口：物理像素摆放、AppBar（固定模式）、浮动模式滑出/滑入、点击穿透；收到 `TaskbarCreated` 广播（Explorer 重启）时 `AppBar.Forget()` 再 `place()` 重新占位 | 窗口位置是物理像素；DIP 换算只在 `toScreen` 一类地方做；Explorer 重启后新的 shell 不认旧 AppBar |
| `stripview.go` | `cardState`、`(*strip).view`、`(*strip).card`、`footer`、`dragPoses`、`windowStill`、`panel`/`showPanel`、`previousStage`/`previousMark`、`cardCloseSize` | 侧边栏界面：卡片、3D 透视、悬停、拖动、右键菜单、底部概览面板；悬停时最前窗口右上角出现圆形关闭按钮（`closeButton`，灰色半透明，指针移上去变红），点击 `actClose` 关掉整张卡片的窗口（组合里的全部），按钮画在卡片的窗口层里随卡片倾斜、缩放；上一个舞台（`stage.Display.Previous()`，任何方式离开舞台都算）的卡片套主题色虚线框（关掉主题色时为白色）（`settings.MarkPrevious`，默认开），虚线框画在卡片的窗口层里，随卡片倾斜、缩放，落地前不显示，几何在 `layout.go` 的 `markRect`；开了“侧边栏显示所有舞台”（`settings.AllStages`）后，台前的应用也是一张卡片（`set.staged`），画成“幽灵”：窗口层按 `StagedOpacity` 整体淡出（外层 `Opacity`、内层 `Cache()`，因为 MyGo 的不透明度是逐图形相乘，叠放的窗口会透出来）、按 `StagedGray` 在静帧上叠一张灰度副本（`windowStill` 的 `gray`，`Grayscale()` 只有开关），右下角带一个绿色对钩徽章（`stageBadge`：MyGo 没有径向渐变，所以用线性渐变的圆片（左上亮、右下深）、上下两道内阴影、上半部渐隐的白色高光和投在卡片上的阴影叠出玻璃质感，白色粗对钩下面垫一份深绿的副本做浮雕；位置 `layout.go: badgeRect` 取窗口层外框 `markRect` 的内侧下角，大小是应用图标的 0.55 倍（`badgeShare`），颜色取浅薄荷绿到系统绿（不用深绿，免得比幽灵本身还抢眼）、夹在侧边栏窗口里；它是正圆的，不放进窗口层的倾斜里（MyGo 里在倾斜的父层中再反向旋转不会抵消，只会再压扁一次，而整个跟着转会压成椭圆），而是用 `layout.go: turnedPoint` 算出卡片的那个角转过去之后的位置，再绕自己的中心转卡片倾斜角的一半（`badgeTurn`，带 `badgePerspective` 的透视）：既不平贴在倾斜的卡片旁边，又不会被压成椭圆；跟着卡片抬起，不跟着窗口层淡出，只随幽灵出现时弹出），点击不切换而是左右震动一下（`shakeSpring`，`Jump("shake")` 之后 `AnimateTo` 弹回，表示“当前就是它”）、悬停不倾斜不抬起不拍照也没有关闭按钮，右键菜单没有“切换/加入/拆开”，拖动只能在本侧边栏里排序（`carddrag.go: aimCard`） | 卡片动作不要直接改模型，走 `a.request`（无头测试靠 `App.act` 截获）；`d.sets` 此时含台前的卡片，“等待的卡片”用 `d.waiting()` 或跳过 `staged` |
| `carddrag.go` | `cardDrag`、`aim`（`aimBack`/`aimSort`/`aimJoin`/`aimMove`）、`aimCard`、`cardDropAt`、`makeRoom`、`joinReach`、`crossReach`、`moveSet`、`reorder` | 拖卡片的去向：回原位、侧边栏内排序、加入舞台、搬到另一台显示器的侧边栏 | 让位布局与飞行层联动，改一处要连着手动验证 |
| `drag.go` | `drag`、`startDrag`、`dragOver`、`windowMoved`、`followMoved`、`leave`、`stripUnder`、`sendToStrip`、`sendAcross`、`dwell` | 拖真实窗口：跨显示器时那边舞台让位、松手、拖进侧边栏、Win+Shift+方向键移动后的跟随 | 只有 `HideInactive` 打开时才停放，否则只更新列表 |
| `layout.go` | `stripLayout`、`layoutLineup`、`markRect`、`fitIn`、`mirror`、`panel`、`cardTilts`、`maxStacked`/`maxIcons`/`minCardScale` 等常量 | 纯几何：卡片位置与缩放、窗口叠放、图标位置、右侧镜像；`head` 让卡片避开侧边栏顶部的舞台窗口列表，`at`（设置 `CardsAt`，30%–70%，默认 50%）是卡片组中心在侧边栏上下的位置：卡片仍按整屏的 `at` 处摆放，离列表不到一个卡片间距时才往下让，让到保持一个间距为止、最多到列表下方剩余空间的 `at` 处（卡片少或列表短时不动；动的时候靠 `cardMotion` 滑过去）；`markRect` 是“上一个舞台”虚线框，跟随卡片倾斜和缩放；`scrollTo` 算出让某张卡片完整进入可视区（列表之下、面板之上）所需的最小滚动量 | 有单测（`layout_test.go`），规则先在这里写并与测试对齐 |
| `media.go` | `media`、`still`、`capture`、`stillHeight`、`flyBudgets`、图标缓存 | 静帧（`PrintWindow`）与图标、内存预算（按 `Quality`）、`LightFlights` | `PrintWindow` 会等目标应用绘制，只能在 goroutine 里做，并且带超时 |
| `iconfit.go` | `iconBounds` | 按 alpha 裁掉图标四周透明边 | 有单测 |
| `lineup.go` | `lineup`、`lineThumb`、`lineTint`、`stageWindows`、`lineupFor`、`lineupView`/`lineTile`/`lineMore`、`placeThumbs`、`sampleTints`、`overLineup`、`bringForward`、`unstage`、`lineupLift`、`closeButton`（卡片和小框共用的圆形关闭按钮） | 舞台窗口列表（`settings.StageList`，默认开）：舞台有 2 个以上窗口时在侧边栏顶部从上到下列出独立的小框（外面不再包一层），每个小框是序号 `1/N` + 图标 + 标题 + DWM 实时缩略图，当前窗口（`a.fg.prev`，否则最前的）用强调色描边；点击 `actFront`，悬停出现关闭按钮 `actClose`，右键可 `actUnstage`；小框在指针下按 `lineupLift`（6%）绕中心放大、阴影加深，DWM 缩略图同步放大；放不下时最后一行“还有 N 个”打开概览 | 缩略图是 DWM 画在侧边栏窗口之上的，MyGo 画的东西（包括 tooltip）会被盖住，所以不用 tooltip；位置每帧随 `reveal` 平移手动同步；顺序在帧间保持稳定（按应用、再按首次出现），点击改变 z 序时列表不跳；几何在 `layout.go` 的 `layoutLineup`，卡片经 `layoutStrip` 的 `head` 参数让出顶部 |
| `tint.go` | `dominantColor`、`hue`、`toOklch`、`tileTint`、`blank` | 列表小框的底色：`lineup.sampleTints` 每 3s（以及缩略图移动或变大小后 250ms）在 goroutine 里用 `winapi.CaptureScreen` 复制侧边栏上那块缩略图的屏幕像素（DWM 已合成好的，不让应用自己重画，不会闪），按色相直方图找主色：排除近黑、近白和灰，越鲜艳权重越大，上下 10%（标题栏、状态栏）×2.5；彩色像素不足 1.2% 视为没有主色，用默认底色。主色在 OKLCH 里定成同样的明度 0.34，白字在任何色相上都看得清；颜色变化 400ms 渐变 | 侧边栏不在屏幕上、飞行中、概览中不取色；锁屏时复制出来是纯色（`blank`），保留原来的颜色 |
| `overview.go` | `overview`、`ovTile`、`overviewPhase`、`openOverview(s)`、`closeOverviews`、`closeWindow`、`accent` | “所有窗口”概览（类 Mission Control）：每台显示器一个窗口，铺开本显示器窗口，点选切换、悬停关闭 | 多显示器同步开关；超时（`overviewTimeout`）必须保证停放的窗口回来；焦点窗口设计见 `newOverview` 注释 |
| `corner.go` | `cornerHint`、`cornerRect`、`cornerNearness`/`boundsNearness`、`cornerBounds`、`joinable`/`strip.setJoined`、`jointView`、`display.flash`、`panelEdge`、`hotCorner`/`cornerReach`/`cornerLinger` | Win11 左下角触发角：不抢焦点的小窗口，靠近显示标记，点击开概览。侧边栏在左侧、任务栏在底部时与侧边栏的概览面板合成一个按钮（`strip.joined`）：面板（`stripview.go: jointFooter`，几何在 `layout.go: jointButton/jointBand/jointGlyph`）贴左边和任务栏顶，触发角从任务栏顶到屏幕底、与面板同宽；悬停/按下经 `strip.footHover/footPress` 与 `cornerHint.hover/pressing` 互通并互相 `redraw`，点击的闪光按 `display.flashAt` 同步；靠近程度都用侧边栏的 `strip.near` | 只有 `winapi.TaskbarCornerFree()` 为真时才盖（轮询里按 1s 缓存，`taskbarCornerFree`）；`a.poll` 先轮询侧边栏再 `watchCorner`，合体时触发角跟着面板出现；合体后 `CornerWidth` 不起作用；判定有单测 |
| `glass.go` | `glass`、`strip.frost`/`reblur`/`buttonScreen`、`frostImage`、`shrinkBy`、`gaussBoxes`、`boxBlur`、`panelFill`、`glassSigma` | 概览面板与合体触发角的毛玻璃：面板从隐藏到出现时（`stripview.go: footer` 里的 `strip.frosted`）在 goroutine 里截下按钮及四周（按最大模糊留边）的屏幕，缩小 1–8 倍后三次横竖盒式模糊（running sum，近似高斯），裁出按钮部分转成 `ui.Bitmap`；面板画上半段，合体触发角画下半段（`Clip` + 圆角）。设置改模糊时 `reblur` 用存下的原图重做 | 只在面板完全隐藏后才重新截，避免把自己截进去；无头测试没有窗口时不截（`s.win == nil`）；算法有单测和 benchmark |
| `peek.go` | `peekStart`、`watchTaskbar`、`peekClass` | 任务栏缩略图 peek 时临时放回被停放的窗口 | 只在指针停在该按钮的缩略图上时放回，避免一次放回太多 |
| `settingsview.go` | `settingsWindow`、`generalPane`/`stripPane`/`stagePane`/`companionsPane`/`aboutPane`、`apply`、`load`、`form.row`/`rowTip`/`info`（标题后的 ⓘ，悬停弹出分条说明） | 设置窗口（Mica，标签页为通用 / 侧边栏 / 舞台 / 例外 / 关于），编辑副本后立即应用；侧边栏页管舞台窗口列表与“上一个舞台”标记，关于页管检查更新并显示红点 | 新设置项：字段 + `Default` + `Normalize` + `Equal`/`Clone` + 对应 pane + `applySettings` |
| `tray.go` | `setUpTray`、`trayMenu` | 托盘图标：左键开设置，右键菜单（找回屏幕外的窗口 / 设置 / 重启 / 退出；重启走 `mygo.App.Relaunch`，先按退出流程放回窗口、释放单实例锁再启动新进程）；有新版本时最上面多一项“新版本 x.y.z…”，带红点图标，点击打开发布页 | 侧边栏和概览都是挂在托盘上的 MyGo flyout |
| `update.go` | `startUpdateChecks`、`checkUpdate`、`newer`、`showUpdate`、`openRelease`、`dotted`/`paintDot`、`firstCheck`/`checkEvery` | 检查更新：启动 10s 后、之后每 6h 在 goroutine 里读 `winapi.SystemProxy()`（Go 默认只认环境变量代理，不认 v2rayN 等设置的系统代理）后调 `update.Latest`（读 `main` 上 `.version` 的 raw 文件，`update.VersionURL`），结果 `post` 回主线程：读到的最新版本存进 `a.published`，比当前新的再存进 `a.latest`；有新版本时托盘图标换成带红点的 64px 版本、tooltip 和菜单第一项写新版本号，设置窗口“关于”标签与页面按 `a.newer()` 画红点；“关于”页同时列出本地版本和最新版本（最新版本旁有红点和“前往下载”，或灰字） | 关掉 `CheckUpdates` 立即去掉红点；`go run` 没有版本号时不联网；网络不通时状态不变，只置 `checkFailed`，“关于”页显示“无法检测新版本，前往查询最新版”，下次打开设置时重试 |
| `debug.go` | `debugf`、`logToFile`、`watchMainThread`、`DebugHang`、`frameStats`、`stopwatch`、`timed`、`tookLong` | 调试日志、看门狗、帧统计、主线程慢任务计时 | 日志文件名和格式见下文；`tookLong` 会打印主线程超过 8ms 的任务 |

测试文件都和被测代码同包：`layout_test.go`、`lineup_test.go`、`tint_test.go`、`stripview_test.go`、`set_test.go`、`corner_test.go`、`flights_test.go`、`iconfit_test.go`、`peek_test.go`、`update_test.go`、`snapshot_test.go`。

## 核心数据结构

- `App`（`app.go`）：`settings` / `store`、`cat`（窗口目录）、`park`（停放）、`media`（静帧与图标）、`displays`、`windows`（最近一次列出的窗口）、`busy` + `running` + `queued`（切换状态）、`drag` / `cardDrag`、`flights`、`tray`、`settingsUI`、`latest` + `checking` + `checkFailed` + `dotted`（更新检查，`update.go`）。
- `display`（`app.go`）：一台显示器的 `mon`、纯逻辑模型 `model stage.Display`、`strip` 窗口、`sets`（卡片，顶在前）、`onStage`、`cards`（每张卡片的帧间状态）、`scroll`、`layout`、`landing`、`dropPoses`、以 `ov` 表示的概览。
- `group`：一个应用在这台显示器上的窗口，按 z 序，`z` 是它最前面那扇窗口在应用之间的位置。`set`：一张卡片，可能包含多个一起上/下舞台的应用；`windows` 前面是每个应用的窗口，后面是其余窗口。
- `cardState`（`stripview.go`）：卡片在帧间要记住的拖动偏移、松手去向、飞行起点（`poses`）、`tilt`/`lift`。
- `strip`（`strip.go`）：窗口与其物理矩形、缩放、`shown`/`away`、`clickThrough`、命中的卡片矩形（DIP）、浮动模式的边缘计时。
- `flights` / `layer` / `flight`（`flights.go`）：每台显示器一层或两层（`under` 放应用自绘阴影），`flight` 是从 `from` 到 `to` 的一扇窗口；`trip` 是一次飞行的一组。
- `lineup`（`lineup.go`）：舞台窗口列表跨帧记住的顺序 `order`、每扇窗口的 DWM 缩略图 `lineThumb`、取色结果的 `lineTint`，以及 `sampling`/`wake`（取色进行中与何时重新取）。`strip.line` 是每台显示器的一份。
- `parking.Service`：`Park` / `Unpark` / `UnparkUnder` / `Rehome` / `Lift` / `Lower` / `RestoreAll` / `CollectEscaped` / `Recover` / `Chase` / `Settle`，每次变动写 `parked-windows.json`。
- `stage.Display`：舞台上应用的有序列表、侧边栏的卡片分组、`above`（从哪张卡片上来的）、`recent`（回退用）、`changedAt`（`Settling` 靠它）、`keep`（`Keep(true)` 后侧边栏把台前的应用也列成一组，在 `sets` 里占一个槽位，`Switch` 不挪任何卡片：离场的应用留在自己的槽位，台前的新组取被点卡片的槽位，加入舞台的取舞台自己的槽位；新应用追加在最下面；`Leave` 原地不动；`Split` 拆不了台前的组）。
- `catalog.Catalog`：`Windows()`（可见的应用窗口，z 序）、`Movable()`（所有可能被移走的窗口）、`Name()`；`catalog.ShadowWindow` 找应用自绘的阴影窗口。

## 关键流程

### 启动

`main.go` 取数据目录 → `app.Start`：加载设置 → `park.Recover()`（上次崩溃留下的窗口先放回）→ 启动看门狗 → 托盘 → 检查更新的定时器（`startUpdateChecks`）→ 飞行层 → `syncDisplays` → `hookEvents` → `startPolling` → 400ms 后 `initStage`（等 AppBar 占位生效再量舞台）→ 1500ms 的兜底 `refresh` tick。

### 一次刷新

`refresh`（事件驱动经 `refreshSoon` 提前）重新列窗口 → 每个显示器 `groupsOf` → `parkStrays`（`HideInactive` 时把不在舞台的窗口停放）→ `reparkEscaped` → 重建 `sets`/`onStage` → 重绘侧边栏。`refill` 处理“舞台的应用全关掉了”，按 `recent` 让上一个应用回来。

### 一次切换

1. 触发：卡片 → `request(actPick)`；Alt+Tab/任务栏 → `onForeground`；新窗口 → `settleForeground`；点壁纸 → `switchStage(d, nil, …)`。
2. `switchStage` 算 departing / arriving；`busy` 时放进 `queued` 并 `flights.interrupt`（空中的窗口原地掉头）。
3. `media.capture` 在 goroutine 给离场窗口拍静帧（带超时）。
4. 更新 `d.model.Switch` → `arrange` → `flights.run`。
5. `handover.takeOff`（停放离场/激活到场）→ `land`（放回到场窗口）→ `stop`/`done` → `runQueued` / `settleForeground`。

### 拖动真实窗口

`EventSystemMoveSizeStart` → `startDrag`；跨到另一台显示器 → `dragOver`（等 `dwell` 后那边舞台让位）；`EventSystemMoveSizeEnd` → `windowMoved`：留在本显示器就保持，落到另一台就加入那边的舞台（`sendAcross`），松手在侧边栏带内则 `sendToStrip`。

### 拖动卡片

`stripview` 记录 `cardState`；`carddrag` 每帧算 `aim`：`aimBack`（几乎没动）、`aimSort`（本侧边栏排序）、`aimJoin`（拖过 `joinReach` 加入舞台）、`aimMove`（跨过 `crossReach` 进另一台显示器的侧边栏）。让位布局在执行前就按目标位置算好，卡片在侧边栏内排序时不交给飞行层。

### 概览与 Win+Tab

`requestOverview` → `openOverview`：`newOverview` 造窗口，把本显示器窗口计入 `tiles`、停放后飞行铺开；`close` 选中的窗口切到舞台，其余回卡片；`closeWindow` 悬停关闭。Win+Tab 由 `winapi/keyboard.go` 的低级键盘钩子截获，`post` 回主线程后对每台显示器各开一个概览，任意一处关闭全部关闭。

### 舞台窗口列表与取色

`strip.stageWindows` 取舞台上的窗口（按应用上舞台的顺序、每个应用的窗口按首次出现，`StageList` 关掉时为空）→ `layoutLineup` 算每个小框和它的缩略图区域 → `lineupView` 画小框，`placeThumbs` 每帧按 `thumbAt` 摆放 DWM 缩略图（DWM 画在侧边栏窗口之上）→ 指针下的小框按 `lineupLift` 放大，缩略图同步放大。缩略图位置或大小变了 250ms 后、之后每 3s，`sampleTints` 用 `winapi.CaptureScreen` 复制屏幕上那块画面，`tint.go` 选主色（排除黑/白/灰），`tintFade` 渐变到新底色。点击发出 `actFront`（`bringForward`），悬停关闭按钮发出 `actClose`，右键发出 `actUnstage`（把应用收进侧边栏）。

### 检查更新与重启

`startUpdateChecks`：启动 10s 后第一次 `checkUpdate`，之后每 `checkEvery`（6h）一次；“关于”页最新版本旁的按钮也调它，可以立刻再查一次（`a.checking` 期间置灰）。它读 `winapi.SystemProxy()`，在 goroutine 里调 `update.Latest`（15s 超时；请求 `raw.githubusercontent.com` 上 `main` 分支根目录的 `.version`，只有一行版本号，不走 api.github.com 的每 IP 每小时 60 次限流；发版时它和 `mygo.json` 一起改，CI 核对），结果 `post` 回主线程：写 `a.published` / `a.latest` / `a.checkFailed`，需要时用 `dotted()` 生成带红点的 64px 托盘图标，托盘菜单与“关于”页据此显示红点。`go run`（没有版本号）或设置里关掉 `CheckUpdates` 时不联网。重启是托盘菜单里的 `mygo.App.Relaunch()`：先按退出流程放回窗口、释放单实例锁，再启动新进程。

### 退出与崩溃恢复

`shutdown` → `park.RestoreAll()`（在关窗口之前）。看门狗 5 秒无响应时直接按日志放回所有窗口并写 `hang-*.log`，主线程恢复后按当前舞台重新停放。

## 常见改动落点

| 想做的事 | 改这里 | 别忘 |
| --- | --- | --- |
| 新增一个设置项 | `settings.Settings`、`Default()`、`Normalize()`（范围）、`Equal`/`Clone`（引用类型）、`settingsview.go` 对应 pane、`app.applySettings` | 配 i18n 文字；`settings_test.go` 加归一化用例 |
| 新增界面文字 | `i18n.Strings` 加字段 + `zh_hans`/`zh_hant`/`en`/`ja` 四张表 | `TestComplete` 检查缺失与格式动词 |
| 新增 Win32 调用 | `internal/winapi`，通用 DLL/proc 放 `procs.go`，专用的就近声明 | 保持无状态；不要在 `stage`/`settings` 里调 |
| 改卡片外观或布局 | `layout.go`（纯几何，先写测试）+ `stripview.go`（绘制） | 右侧镜像要一起测 |
| 改舞台窗口列表 | `layout.go` 的 `layoutLineup` + `lineup.go`（尺寸、`lineupLift`、`sampleTints` 的节奏） | DWM 缩略图会盖住它下面的 MyGo 内容；`flights.cardPoses` 也要用同一个 `head` |
| 改更新检查 | `app/update.go` 的 `firstCheck`/`checkEvery`/`checkTimeout`/`dotted`，`internal/update`（`VersionURL`、`ReleasesURL`） | 网络请求只在 goroutine；`go run` 没有版本号，不联网；`.version` 的格式或位置变了，旧版本读不到，会显示“无法检测新版本” |
| 改托盘菜单 | `tray.go` 的 `trayMenu` | 文字走 `a.tr()`；重启用 `mygo.App.Relaunch()`，不要自己起进程 |
| 改切换动画节奏 | `flights.go` 的 `flightTime`/`takeOffDelay`/`landingDelay`/`fadeTime`，`switch.go` 的 `handover` | 录屏逐帧看阴影有没有跳 |
| 新增卡片动作 | `switch.go` 的 `action` 常量、`request`、具体函数；`stripview.go` 菜单里发起 | `App.act` 让无头测试能截获 |
| 改概览 | `overview.go`（布局常量、`ovTile.at`、`close`/`land`） | 超时必须保证窗口放回；多显示器同步 |
| 改舞台规则 | `stage.go` + `stage_test.go` | 不要在 `app` 里写规则判断 |
| 改“侧边栏显示所有舞台”（台前的幽灵卡片） | 规则在 `stage.go` 的 `Keep`/`switchKept`，外观在 `stripview.go` 的 `card`/`windowStill`，滚到离场卡片在 `switch.go` 的 `reveal` + `layout.go` 的 `scrollTo`，设置在 `settingsview.go` 的侧边栏页 | 台前窗口的静帧不要周期性重拍；飞行与交接没有自动化测试，手动验证：点卡片、快速连点、卡片很多需要滚动、概览里选一个、Shift+点击加入、开关来回切 |

## 调试

| 环境变量 | 作用 |
| --- | --- |
| `STAGEMANAGER_APP_FILTER` | 只管理这些 exe（逗号分隔），开发时不会动到你自己的窗口 |
| `STAGEMANAGER_DATA_DIR` | 设置、停放日志、调试日志的目录（默认 `%LOCALAPPDATA%\Stage Manager`） |
| `STAGEMANAGER_DEBUG` | 输出决策日志到控制台和 `debug.log`（上次的 `debug.prev.log`）；此时第二个实例传 `--debug-hang` 可卡住主线程 12 秒验证看门狗 |
| `STAGEMANAGER_SNAPSHOT_DIR` | `go test ./internal/app -run TestSnapshots` 把侧边栏和设置窗口渲染成 PNG |
| `MYGO_FRAME_STATS` | MyGo 把耗时超过 8ms 的帧的构建/布局/绘制/提交耗时打进日志（`start-stage-manager.bat` 会开） |

日志里值得先看的行：`flights: ... N frames in ..., longest gap ...`、`strip ...:`、`flight layer ...:`、`overview ...:`、`main thread: X took T`、`... took T: a 1ms, b 2ms`、`capture [...]`、`update: latest .../update: ...`（检查更新）、`lineup ...: tints N`（取色）、`lineup: ... clicked`、`strip ...: Explorer restarted, reserving the band again`。

## 测试

| 包 / 文件 | 覆盖 |
| --- | --- |
| `stage` | 舞台与侧边栏规则、`Prune`/`Insert`/`Move`/`Split` |
| `settings` | 归一化、旧设置迁移、保存与读取 |
| `i18n` | 四种语言字段齐全、格式动词一致 |
| `update` | 版本号比较、解析 `.version`（带 BOM、换行、`v` 前缀）与失败情形（限流、404、不是版本号的页面）、系统代理（`ProxyServer`/`ProxyOverride`）解析、经代理请求、代理失效时直连 |
| `app/layout` | 卡片布局、叠放、镜像、图标位置 |
| `app/tint` | 主色：橙/蓝标题栏 + 彩色代码的深色编辑器、白底、灰/黑/白、零星彩色字；OKLCH 换算；不同色相底色明度一致；锁屏纯色判定 |
| `app/lineup` | 舞台窗口列表的几何（放不下时“还有 N 个”）、卡片让出顶部、顺序稳定、点击/右键/关闭按钮发出的动作、关闭开关与概览时隐藏 |
| `app/stripview` | 无头渲染卡片与面板，经 `App.act` 截获动作；上一个舞台的标记跟随切换、关掉设置后消失 |
| `app/staged` | `AllStages`：`arrange` 把台前的卡片列在槽位里、切换不动卡片、开关来回；离场卡片在可视区外时 `reveal` 滚过去；台前卡片点击无反应、菜单里没有切换、只能排序；半透明与灰度的像素 |
| `app/set` | 跨应用卡片的窗口顺序 |
| `app/corner` | 触发角几何、靠近程度、点击开概览 |
| `app/flights` | `around` 给被拖窗口让出的“洞” |
| `app/iconfit`、`app/peek`、`app/update` | 图标裁边、peek 的按钮匹配、托盘红点图标 |
| `app/snapshot` | 需要 `STAGEMANAGER_SNAPSHOT_DIR`，否则跳过 |
| `winapi/monitor` | 显示器几何与 DPI |

没有自动化测试、必须手动验证的：`catalog`、`parking`、飞行动画、窗口交接、跨显示器拖动、拖动卡片。清单见 `AGENTS.md` 的“测试”一节。

## 容易踩的坑

- 主线程不能做可能阻塞的事（`PrintWindow`、等待别的进程、长循环）。看门狗会在 5 秒后直接动手。
- 不要用锁“修”并发：后台结果用 `post`/`after` 回主线程；唯一例外是 Win+Tab 的低速键盘钩子线程。
- 隐藏窗口一律走 `parking.Service`，不要自己 `SetWindowPos` 移出桌面，否则崩溃后窗口回不来。
- 坐标：系统侧物理像素，MyGo 窗口内 DIP，换算只在 `strip.toScreen`、`mon.Scale()` 一类地方。
- 整屏动画用 `pacer`；飞行层空了以后要留 2 秒（`layerLinger`），不要在动画进行中显示整屏窗口。
- 拖动中的窗口上面不要压飞行层（系统很少把被压住的画面送上屏幕），用 `hole` 避开。
- 卡片显示的是静帧，停放中的窗口不会更新；`PrintWindow` 拍不到内容时退化成图标卡片。
- 舞台窗口列表的缩略图是 DWM 画在侧边栏窗口之上的：MyGo 的 tooltip、遮罩都盖不住它，位置只能每帧用 `placeThumbs` 同步；隐藏/移动侧边栏时要 `dropThumbs`。
- 取色用 `winapi.CaptureScreen` 读 DWM 合成后的屏幕，不要改用 `PrintWindow` 让应用重画：那会让应用闪一下。
- 联网只发生在 `app/update.go` 的 goroutine 和 `internal/update` 里；Go 默认只认环境变量代理，所以要自己 `winapi.SystemProxy()` 读系统代理，读不到时才直连。
