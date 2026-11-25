# 任务：审查 Stage Manager for Windows（Go 版）中的"不整洁设计"与代码坏味道（只读，不修改代码）

你是一名资深 Go / Win32 桌面开发工程师，要对 `E:\github_repos\stage-manager-for-windows\stage_mgr_go` 做一次整洁代码审查。
目标是找出**真正会让以后的修改变难或埋下 bug 的设计**，不是挑格式毛病。最后交一份可以直接据此动手的报告。

## 开始前必须先读
1. 根目录 `AGENTS.md`：线程模型、停放、坐标、分层、设置、界面文字这几条约束，以及"当前进行中 / 已知问题"里每一条刻意的设计（它们不算坏味道）。
2. `docs/CODEMAP.md`：逐文件符号表、分层依赖表、各流程（启动 / 刷新 / 切换 / 拖动 / 概览 / 舞台窗口列表 / 检查更新），以及"改什么动哪里"的对照表。
3. `stage_mgr_go/README.md`：构建与安全运行方式。
4. 遇到阴影、闪烁、时序相关代码时，翻一下 `.codebuddy/plans/` 里之前的分析，那里解释过的时序不要当成魔法值报告。

## 范围
- 审查：`stage_mgr_go/main.go`、`stage_mgr_go/internal/` 下全部包（`winapi`、`catalog`、`parking`、`stage`、`settings`、`i18n`、`update`、`app`）。
- `*_test.go` 只看测试之间明显重复的工具代码（造 `App` / `display`、假窗口、无头渲染的样板），单独列一节。
- 不看：`stage_mgr_go/build/`、`其他项目参考代码/`、`.codebuddy/`、`.claude/`、Go 模块缓存。
- **不要修改任何文件。** 可以跑 `go build ./...`、`go vet ./...`、`go test ./...`、`go tool mygo vet .`（都在 `stage_mgr_go/` 下，几秒跑完）；**不要运行应用本身**（`go run .`、`go tool mygo dev`、exe、`start-stage-manager.bat`）：它会把真实窗口移到桌面外。

## 已经处理过的，不要重复报告
- （每轮审查前把上一轮已修的项补在这里，例如"某判断已统一成 `xxx.go` 的 `yyy`"。）
如果这些已改的地方还有漏网的调用点，可以报告。

## 要找的坏味道（每类都附了线索，不限于这些）

### A. 重复的"知识"（最重要）
- 同一条**业务规则**写了多次。比如同一组状态 / 模式判断（`x == a || x == b`）、"侧边栏里等待的卡片"应当走 `d.waiting()` 或跳过 `staged` 却各处手写、同一种窗口过滤条件、同一组 `SWP_*` / `WS_EX_*` 标志组合、同一种"找窗口所在显示器"的写法，散落在多个文件里。改一处就得找全所有处（散弹式修改）。
- 复制粘贴的代码块：几行到几十行几乎一样，只有一两个值不同（例如侧边栏、概览、舞台窗口列表、触发角各自算一遍悬停 / 按下 / 动画进度）。
- 对同一个判别值（动作种类 `act*`、拖动去向 `aim`、侧边栏位置左右、浮动 / 固定模式、设置页 `nav`）的平行 `switch` / `if` 链出现在多个地方。新增一种动作、去向或页面时要改 N 处，数一下要改哪几处。
- 同一个结构或常量在多个包里各自定义一份（例如 `app` 和 `winapi` 各有一份几何换算、两处各写一份同样的 Win32 结构体或常量）。
- 同一件事有两种写法并存：例如动画要帧有的用 `pacer`、有的用 `AnimationFrame`；回主线程有的用 `post`、有的直接 `mygo.RunOnMain`；DPI 换算有的走 `strip.toScreen` / `mon.Scale()`，有的就地乘除。
- "新增一个设置项"本身就是一次散弹式修改（`Settings`、`Default()`、`Normalize()`、`Equal/Clone`、`settingsview.go`、`applySettings`）。数出真实要改几处，看有没有哪一处容易漏、漏了也不报错。
- 用户可见文字没走 `a.tr()`：代码里写死的中文 / 英文字符串、`fmt.Sprintf` 拼出来的界面文案、托盘提示、错误对话框。

### B. 魔法值与"字符串 / 整数当类型用"
- 没有说明的魔法值：`120 * time.Millisecond`、`time.Sleep`、像素尺寸、圆角、阴影偏移、重试次数、阈值、内存 / 字节上限。AGENTS.md 要求"常量旁注明取值理由（如 `takeOffDelay`）"，没注明的就是问题。
- 一组封闭取值没有用具名类型约束，而是裸 `int` / `string` / `bool` 传来传去与字面量比较，传错了编译器也发现不了。Go 的惯例是 `type kind int` + `const ( ... iota )` 或具名 `string` 类型的常量组。
- 同一个环境变量名（`STAGEMANAGER_*`）、设置文件名、日志文件名、窗口类名、注册表路径、JSON 字段名，在**同一语义下**于多个文件重复书写。
- Win32 SDK 常量（`SWP_NOZORDER`、`WM_NCHITTEST` 等）按 SDK 名字声明在 `winapi` 里的不算魔法值；但在 `app` 里直接写数字 `0x0010` 的算。

### C. 类型安全被削弱
- `any` / `interface{}`、不带 `, ok` 的类型断言、为了"通用"而定义的空接口参数。
- `unsafe.Pointer` / `uintptr`：除了紧贴 `syscall` 调用的必要转换，检查有没有把 `uintptr` 存下来再转回指针（GC 可能已移动或回收）、结构体布局与 Win32 不一致（缺 `cbSize`、对齐）、`go vet` 的 `unsafeptr` 抓不到的写法。
- `proc.Call(...)` 的三个返回值：`r1` 才是 Win32 的结果，`err` 几乎总是非 nil（来自 `GetLastError`）。检查是否误用 `err != nil` 判断失败，或者该检查 `r1` 却直接丢掉。
- 易混的同类值都是裸类型：物理像素的 `winapi.Rect` 与 DIP 的 `ui.Rect`、`HWND` 与普通 `uintptr`、显示器句柄与窗口句柄、进程 id 与窗口 id。参数表里互相传反编译器看不出的地方，建议用具名类型。只给真正易混的加，不要给每个 `int` 都加。
- 可选字段过多的"大结构体"（零值含义靠约定、几个字段只在某种模式下有意义），调用方要靠约定才知道哪些组合合法。在 Go 里应拆成几个具体类型，或用一个小接口区分。
- 对封闭取值集合的 `switch` 没有 `default` 兜底：新增成员时编译器不会提醒，只能靠人再找一遍。
- 外部数据（`settings.json`、`parked-windows.json`、`.version` 的 HTTP 响应、注册表、窗口标题 / 类名）没有校验就直接使用。

### D. 函数与类型的形状
- 过长的函数（大概超过 80 行，按实际判断）、职责混杂的"上帝文件 / 上帝结构体"。重点看 `app.go`、`flights.go`、`settingsview.go`、`overview.go`、`stripview.go`，以及 `App`、`display` 两个结构体的字段数量。
- 参数过多，或布尔标志参数（`foo(d, w, true, false)`）。
- 超过三层的嵌套、在构建 UI 的表达式里写一长串条件。
- 数据泥团：同一组参数总是一起传（`d, rect, scale`、`from, to, progress`），应该合成一个结构体。
- 依恋情结：一个函数大部分时间在读写另一个结构体的字段（例如 `stripview` 里的函数在操作 `flights` 的状态）。
- 抽象的成本：只有一个实现的接口、只有一个调用方的转发函数、为"以后可能"加的从未用过的参数或字段，都是成本。判据：抽象要么挡住一处真实变化（例如为了无头测试替换 Win32），要么消除两处以上重复，否则不成立。每条注明该"删 / 并 / 下沉"。
- 绕过已有的入口：项目已经为某件事提供了唯一入口，某处却直接调底层。重点检查：
  - 移走窗口不走 `parking.Service`、直接 `SetWindowPos` 到桌面外（崩溃后回不来）；
  - 卡片动作不走 `a.request`、切换不走 `switchStage`；
  - 拍静帧不走 `media.capture`（goroutine + 超时）而在主线程 `PrintWindow`；
  - 界面文字不走 `a.tr()`；整屏动画不走 `pacer`。
  判据：在崩溃恢复、看门狗、无头测试这三种情况下，这段代码的行为和走入口时一样吗？

### E. MyGo 界面专属
- UI 状态只是模型的副本：卡片、概览、舞台窗口列表自己记了一份"哪些窗口在哪"，而不是每次从 `d.model` / `d.sets` 派生，两份会不同步。
- 每帧构建时做了重活：分配大切片、缩放图片、调 Win32 查询、读文件。判据：飞行或拖动时每秒几十上百帧，这段代码会不会出现在 `main thread: X took T` 里。
- 定时器、动画、`after(d, fn)` 回调没有在窗口关闭、显示器移除、设置变化时取消，或者回调里不检查对象是否还在用（见 K）。
- 延时回调不检查等待期间状态是否已被别的操作改变，比如悬停延时展开叠加拖动开始、延时收起叠加再次悬停。
- 交互中的状态（悬停、按下、拖动、滚动位置、设置页当前页）放在会被重建的视图里，切换显示器 / 改设置后丢失。
- DWM 缩略图、截图纹理、`Cache()` 的图层在隐藏后没有释放（`dropThumbs` 这类收尾有没有每条路径都走到）。
- `go tool mygo vet .` 已经能抓的生命周期问题不要重复报；它抓不到的模式（例如闭包捕获了旧的 `*display`）才报。

### F. 包边界与分层（以 AGENTS.md 和 CODEMAP.md 的分层表为准）
- `winapi` 持有应用状态（包级可变变量、缓存），或者知道舞台、卡片。
- `stage` / `settings` / `i18n` / `update` import 了 `winapi` 或出现 `HWND`；`catalog` / `parking` 知道舞台、卡片、设置界面。
- `app` 里散落 Win32 细节：直接 `syscall` / `proc.Call`、手写 Win32 结构体，而不是加到 `winapi`。
- 纯几何没放进 `layout.go`，而是写在 `stripview` / `overview` / `flights` 里，导致无法单测。
- Windows 专用文件缺 `//go:build windows`。
- 包级可变全局变量（`var cache = map...`）：谁在什么线程读写？
- 变成杂物堆的文件（不相关的东西堆在一起）。注意 `debug.go` 有意把调试日志、看门狗、帧统计放在一起，判断它是否真的已经混乱再报。
- 循环依赖在 Go 里编译不过，不用找；但要找"为了绕开循环依赖而把类型塞错包"的情况。

### G. 错误处理与资源
- 吞掉错误：`_ = f()`、`if err != nil { return }` 却不记日志、`json.Unmarshal` / `os.WriteFile` / `os.Rename` 的错误被忽略，且没有注释说明为什么可以忽略。同类操作有的报错有的静默。
- 资源生命周期不对称（这类问题在本项目里会直接拖垮系统，按 P1 看）：
  - `SetWinEventHook` / `SetWindowsHookEx` ↔ `UnhookWinEvent` / `UnhookWindowsHookEx`；
  - `GetDC` / `CreateCompatibleDC` / `CreateDIBSection` / `CreateCompatibleBitmap` / `CopyIcon` / `LoadImage` ↔ `ReleaseDC` / `DeleteDC` / `DeleteObject` / `DestroyIcon`（GDI 句柄每进程上限约 10000，泄漏到上限后整个应用绘制失败）；
  - `DwmRegisterThumbnail` ↔ `DwmUnregisterThumbnail`；`OpenProcess` ↔ `CloseHandle`；注册表键 ↔ `Close`；
  - AppBar 注册 ↔ 注销；托盘图标添加 ↔ 删除。
  检查错误路径和提前 `return` 的路径是否也释放了。
- goroutine 泄漏：后台 goroutine 没有退出条件（`update.go` 的周期检查、看门狗、钩子线程），退出流程（`--quit`、Ctrl+C）时会不会卡住或泄漏。
- 无界增长：静帧 / 图标缓存（`media.go` 的内存预算是否覆盖所有缓存）、`parked-windows.json` 日志、调试日志文件、`hang-*.log`、按 HWND 建索引却从不删除已关闭窗口的 map。判据：开机一周、开关几百个窗口后，内存或磁盘会不会只增不减。

### H. 命名与注释
- 同一个概念在不同文件里叫不同名字（例如 set / group / card / stage 混用指同一样东西），或者名字和实际行为不符。项目风格就是短名字（`park`、`fg`、`air`），**短不是问题，含义不清或前后不一才是问题**。
- 否定式布尔（`noX`、`notReady`、`!disabled`）。
- 只是复述代码的注释；与代码已经矛盾的过时注释；陈旧的 TODO（核对它说的事是否已经做完）。
- AGENTS.md 要求代码注释用英文完整句子、说明"为什么"：出现中文注释或只有半句的注释，算违反约定。
- 导出了但只在包内用的标识符（应该小写），或者反过来。

### I. 死代码
- 没有任何引用的函数、方法、类型、常量、结构体字段；注释掉的代码块；永远走不到的分支；已经没人设置的开关、设置项或参数。
- 用 grep 确认没有引用之后再报告。注意隐式引用：`*_test.go`、`json:"..."` 标签（设置文件里的旧字段可能需要保留以兼容）、`mygo.json`、CI 脚本、`start-stage-manager.bat`、环境变量。
- 可选：如果能联网，`go run golang.org/x/tools/cmd/deadcode@latest ./...` 给出候选，但结果仍要逐条 grep 确认（它不认识测试和反射）。

### J. 状态机与不变式
- 隐式状态机：同一结构体上多个布尔 / 计数（`busy`、`queued`、`pending`、`landing`、`air`、`linger`、`gen`、`seq`）共同编码一个状态，合法组合只有少数，判断散落在各个回调里。判据：两个字段之间存在"不能同时为 true / `b` 只能随 `a` 变"这类关系，就该收成一个具名状态类型（`type phase int`）加集中的转换函数。重点看切换排队与打断（`switch.go`）、飞行层显示 / 隐藏 / 逗留（`flights.go`）、拖动（`drag.go`、`carddrag.go`）、侧边栏浮动显示（`strip.go`）。
- 时序假设：`post(fn)` 之后立刻读状态（`fn` 是下一拍才在主线程执行的）；回调里直接改另一个子系统的私有字段。
- 只为测试而存在的生产代码：专供测试预置数据的 setter、导出、开关。区分清楚：`App.act` 截获卡片动作这类已在 AGENTS.md 写明的测试钩子不算。
- 原地修改共享数据（Go 特有的切片 / map 别名）：返回内部切片给调用方、`append` 到别人传进来的切片、模型的切片被 UI 持有后又被模型改写、`calmShared` 这类原子指针指向的数据被原地改而不是整份替换。判据：这个切片 / map 还有谁拿着？
- 不变式没有归属：像"所有 `App` 状态只在主线程读写""每次停放都写日志""每台显示器一个舞台和侧边栏""飞行层始终在最上层"这种约束，要么只在一处强制、要么到处检查、要么没人检查。报告它现在由谁保证，还是只写在 AGENTS.md 里。
- 可驱动性：逻辑直接依赖 `time.Now()`、包级单例、直接调 Win32，导致只能真实运行、靠 sleep 才能验证。判据：这个行为能不能像 `stage`、`layout`、`staged_test.go` 那样无头测试？不能则是结构问题（不要求本轮解决，但要报出来）。

### K. 并发与时序（以 AGENTS.md 的线程模型为准）
- **goroutine 里直接读写 `App` / `display` / 模型的字段**，而不是把结果 `post` 回主线程。这直接违反线程模型，按 P1 报。注意闭包捕获：goroutine 里读 `a.settings.X` 也算。
- goroutine 结果回到主线程时，把结果写回可能已经被替换的对象（显示器被拔掉或重建、设置改变、那次切换已被后来的请求接管、窗口已关闭）。判据：`post` 回来时，捕获的 `*display` / `HWND` / 请求还是当初那个吗？有没有代数或比较保护？
- 缺少取消：长任务（拍静帧、截屏取色、毛玻璃截图、检查更新）没有超时或取消，或有但某条路径绕过了它。
- 低级钩子线程（`keyboard.go`、`minbutton.go`）里出现锁、channel 阻塞、等待、分配大对象，或回调读了非原子的共享状态。AGENTS.md 明确写了重入锁死的事故，这里只报新出现的违反。
- 用 `sync.Mutex` 去"修"主线程状态的并发，而不是回到主线程（AGENTS.md 明确不要这么做）。
- 顺序假设：两个异步结果（静帧完成与下一次切换、显示器变化与刷新、设置保存与应用）假设先后到达，但没有排队或版本号保护。
- 注意：`go test -race` 需要 cgo，本项目不可用，只能靠读代码。

### L. 持久化与兼容（小类）
- `settings.json`：**Go 的零值陷阱**——新增一个默认为 `true` 或非零的字段，旧设置文件里没有它，`json.Unmarshal` 后是零值，用户升级后行为静默变化。检查 `Default()` 是否先填好再 `Unmarshal`、`Normalize()` 是否覆盖所有有范围的字段。
- `parked-windows.json`：旧版本崩溃留下的日志，新版本启动恢复时能不能读；字段改名或语义变化时旧日志会怎样（读不出来就意味着用户的窗口回不来，按 P1 看）。
- 写文件是否原子（写临时文件再 `Rename`），中途崩溃会不会留下半个 JSON。
- 有没有 version 字段；没有时，判断以后改格式时能不能区分新旧数据。

## 判断标准：避免误报（这部分和找问题一样重要）
- **字面量相同不等于知识重复。** 比如两处都写了 `200 * time.Millisecond`，一个是悬停延时、一个是飞行时长，只是碰巧相等，合成一个常量反而会制造虚假的耦合。只有"同一条规则、同一个语义、改一处必须改所有处"才算重复。
- 封闭取值集合用 `type X int` + `iota` 或具名 `string` 常量，这是 Go 和本项目的惯例；不建议引入代码生成器或第三方 lint 工具。
- 不要为了长得像而抽象，也不要为了去重而引入接口或泛型。两段代码相似但以后会各自演化，就不算问题。
- 不报告 `gofmt` / `go vet` / `go tool mygo vet` 已经能处理或已在 CI 里检查的问题。
- `winapi` 里成片的 `proc` 声明、按 SDK 原样镜像的结构体、紧贴 syscall 的 `unsafe.Pointer` 转换是必要的，不算坏味道。
- AGENTS.md 中标为有意的设计不算问题，例如：钩子回调只用原子变量不加锁；同进程内焦点移动不拍静帧（`sameProcess`）；文件资源管理器的窗口照样拍静帧；放回时带 `SWP_FRAMECHANGED`；飞行层留在最上层并用 `hole` 避开被拖窗口；`layerLinger` 2 秒和 `standBy` 提前显示；`pacer` 代替 `AnimationFrame`；毛玻璃是出现那一刻的静态截图；`Split` 跟着切换请求在 `commit` 里生效；"幽灵"卡片用 `Cache()` 和叠灰度副本实现；`.codebuddy/plans/` 里分析过的阴影时序常量。
- 需要大规模重构的问题（拆分 `App` / `display`、拆分上帝文件、把隐式状态机整体改写），只报告并标为"单独立项"，不要展开设计细节。
- 不建议引入新依赖（除 MyGo 和 `golang.org/x/image` 外），不建议用 cgo。

## 工作方法
1. **用 team 模式并行审查**：把下面的区域拆给多个子代理同时扫描，每个子代理只负责自己领到的区域、只读不写；回报必须带出现次数和全部位置。主上下文统一合并、去重、复核后再定优先级（P1 级发现必须由主上下文亲自读代码确认）。没有 team 能力时，按同一顺序依次扫描。
   - 区域：
     1. `internal/winapi`（资源配对、`Call` 返回值、`unsafe`、钩子线程）
     2. `internal/catalog` + `internal/parking` + `main.go`（停放日志、崩溃恢复、退出流程）
     3. `internal/stage` + `internal/settings` + `internal/i18n` + `internal/update`（纯逻辑、持久化兼容、零值陷阱）
     4. `app` 的切换与动画：`switch.go`、`flights.go`、`pacer.go`、`media.go`、`peek.go`
     5. `app` 的输入：`events.go`、`drag.go`、`carddrag.go`
     6. `app` 的侧边栏与面板：`strip.go`、`stripview.go`、`layout.go`、`lineup.go`、`tint.go`、`overview.go`、`corner.go`、`glass.go`、`iconfit.go`
     7. `app` 的其余：`app.go`、`settingsview.go`、`tray.go`、`update.go`、`debug.go`
2. 用 grep（ripgrep 语法）找线索，再**读上下文确认**。可用的线索包括：
   - `== \w+ \|\| [\w.]+ == ` 与 `case .*, .*, `（重复的或条件 / 多值 case）
   - `\d+\s*\*\s*time\.(Millisecond|Second)|time\.Sleep|time\.Duration\(\d`（魔法时长）
   - `\bany\b|interface\{\}|\.\([\w.*]+\)$`（空接口、不带 ok 的断言）
   - `unsafe\.Pointer|uintptr\(`（不安全转换，只看 `winapi` 之外或存下来再用的）
   - `\.Call\(`（看返回值是否被正确使用）
   - `_ = |_, _ = |, _ := `（丢弃的错误）
   - `go func|go \w+\(`，再看函数体里有没有不经 `post` 碰 `a.` / `d.`
   - `post\(|RunOnMain|after\(`（回主线程与延时回调，看捕获了什么）
   - `SetWindowPos|MoveWindow|ShowWindow|SetWindowPlacement`（在 `parking` / `winapi` 之外出现时，是否绕过停放）
   - `PrintWindow|CaptureScreen`（是否都在 goroutine 里且带超时）
   - `SetWinEventHook|SetWindowsHookEx|GetDC\(|CreateCompatibleDC|CreateDIBSection|DeleteObject|ReleaseDC|DwmRegisterThumbnail|DwmUnregisterThumbnail|DestroyIcon|CloseHandle`（资源配对，数清两边的次数）
   - `Scale\(\)|\* *scale|/ *scale|\b96\b`（DPI 换算散落）
   - `"[^"]*\p{Han}`（非 `i18n` 包、非测试文件里写死的中文字符串）与 `//.*\p{Han}`（中文注释）
   - `busy|queued|pending|landing|linger|gen\b|seq\b|epoch|stale`（同一结构体上用字段组合出的隐式状态）
   - `STAGEMANAGER_|\.json"|\.log"`（环境变量、文件名的重复书写）
   - `time\.Now\(\)`（可驱动性）
   - `^var \w+ = |^var \($`（包级可变状态）
   - `json:"`、`Default\(\)|Normalize\(`（设置字段与默认值是否一一对应）
   - `return [\w.]+\.\w+$` 与 `append\(\w+\.\w+`（内部切片外泄、别名修改）
   - `TODO|FIXME|XXX|HACK`
   - 对同一判别值的 `switch` 与 `case act`、`case aim` 一类
3. 每一条发现都要**数出出现次数并列出所有位置**，这样修的人能一次改完。
4. 每条都要问自己：这条问题会让哪一种具体的未来修改变难，或者会招来哪一种具体的 bug？说不出来就不要报。

## 输出格式
先给一段三到五句话的总评：整体整洁度，以及最值得优先处理的三个问题。然后按优先级（P1 → P3）分组列出发现，每条按下面的格式：

- **[ID] 标题**（类别字母，如 A / B）
  - 位置：`stage_mgr_go/internal/app/xxx.go:行号`，所有出现处都列出，注明共几处
  - 证据：最小的代码片段
  - 问题：会让什么修改变难，或招来什么 bug（要具体）
  - 建议：具体做法，包括新函数或类型叫什么、放在哪个包哪个文件（遵守 AGENTS.md 的分层：Win32 进 `winapi`，规则进 `stage` 并先写测试，几何进 `layout.go`，文字进 `i18n.Strings` 并补齐四种语言）
  - 影响面 / 风险：改动涉及几个文件；是否触及 `catalog`、`parking`、飞行动画或窗口交接（这些没有自动化测试，要按 AGENTS.md"测试"一节的清单手动验证）；改了 UI 要跑 `go tool mygo vet .`
  - 工作量：S / M / L
  - 防线（规则类、重复出现的发现必填）：把它变成类型约束、单测或"架构测试"的具体落点，让同类问题下次在编译期或 `go test` 失败。可用的手段都只依赖标准库：
    - 具名类型 / 不导出的构造函数，让错误的组合编译不过；
    - 像 `i18n.TestComplete` 那样的穷举测试（每个枚举值都有处理、每个设置字段都有默认值和界面）；
    - 用 `go/parser` + `go/ast` 写的架构测试（例如禁止 `stage` / `settings` import `winapi`、禁止 `parking` / `winapi` 之外调用 `SetWindowPos`、禁止 `i18n` 之外出现中文字符串字面量）。

优先级定义：
- P1：会导致或已经埋下 bug（窗口回不来、句柄泄漏、跨线程读写、升级后设置静默变化），或者违反 AGENTS.md 的约束。
- P2：明显增加修改成本的重复或混乱。
- P3：可读性小问题。

最后加两节：
- **"考虑过但不建议改"**：列出看起来像问题、你判断后决定不报的情况，以及理由。这一节用来校准判断。
- **"需单独立项的大重构"**：只列出问题和影响，不展开方案。

报告控制在大约 30 条以内，宁缺毋滥。不确定的发现标为"待确认"并说明需要确认什么（例如"需要多显示器手动验证"）。
