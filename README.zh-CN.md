<p align="center">
  <img src="StageManager/Assets/stagebar-icon.png" width="128" height="128" alt="StageBar 图标">
</p>

<h1 align="center">StageBar</h1>

<p align="center">
  一个受 macOS 台前调度启发、适用于 Windows 10 和 Windows 11 的实时窗口切换工具。
</p>

<p align="center">
  <strong>简体中文</strong> · <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/renkai9418/stage-bar/actions/workflows/build.yml"><img src="https://github.com/renkai9418/stage-bar/actions/workflows/build.yml/badge.svg" alt="构建状态"></a>
  <a href="https://github.com/renkai9418/stage-bar/releases"><img src="https://img.shields.io/github/v/release/renkai9418/stage-bar" alt="最新版本"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/renkai9418/stage-bar" alt="MIT 许可证"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%2F11-0078D4" alt="Windows 10/11">
</p>

> [!NOTE]
> StageBar 当前处于早期预览阶段。`0.0.1` 版本重点提供稳定的 DWM 实时预览基础，以及台前调度的核心交互体验。

## 项目简介

StageBar 把 macOS 的台前调度带到 Windows。当前使用的应用留在桌面中央的“舞台”上，其余应用按应用分组收进左侧条带。切换应用时，当前窗口会缩进被点卡片的位置，被选中的应用则从该卡片中展开回到原位，条带其余卡片保持不动。

预览使用 Windows 桌面窗口管理器（DWM）的缩略图接口，由系统直接合成，不需要反复截图或强制目标窗口重绘。

提供两种展示模式：

- **固定模式：** 通过 Windows AppBar API 预留左侧工作区，最大化窗口会使用剩余区域，不会遮挡 StageBar。
- **悬浮模式：** 与 macOS 一致，左侧有空间时条带保持显示，被舞台上的窗口遮挡时自动让开；鼠标移到屏幕左边沿即可唤出。

## 下载安装

请从 [GitHub Releases](https://github.com/renkai9418/stage-bar/releases/latest) 下载最新版：

- [StageBar Windows x64 安装程序](https://github.com/renkai9418/stage-bar/releases/latest/download/StageBar-Setup-0.0.1-x64.exe)

安装包采用自包含发布，目标电脑不需要单独安装 .NET。默认仅为当前用户安装，不需要管理员权限。

> [!WARNING]
> `0.0.1` 版本尚未进行代码签名，Windows SmartScreen 可能显示“未知发布者”提示。运行前请核对 Release 页面提供的 SHA-256。

## 主要功能

- 台前调度：桌面上只保留当前应用，其余应用收进左侧条带
- 切换动画：离开的窗口缩进条带，选中的应用从卡片展开回原位
- 条带使用可打断的弹簧动画：卡片滑动、出现、消失和悬停反馈
- 卡片像 macOS 条带一样带透视倾斜、柔和阴影和大号应用图标；鼠标悬停时卡片会转向你
- 同一应用的多个窗口合并为一张叠放预览卡片
- 离开舞台的应用替换被点卡片的位置，条带顺序保持不变；Alt+Tab、任务栏点击和新打开的应用会立即上台
- 把卡片拖到桌面上，可将该应用加入当前舞台
- 点击桌面壁纸，把当前舞台收进条带
- 隐藏的窗口保持原有大小和状态并瞬间返回；StageBar 退出或崩溃后会自动还原
- 使用系统合成器提供稳定画面，不使用 `PrintWindow` 重绘循环
- 固定模式通过 AppBar 预留桌面工作区
- 悬浮模式支持左侧边缘唤出、自动隐藏和过渡动画
- 可配置展示宽度与应用图标大小
- 应用变多时卡片自动缩小，整条条带始终放得下
- 鼠标滚轮浏览卡片
- 应用图标独立显示在 DWM 预览上层
- 多显示器定位与 Per-Monitor DPI 感知
- Windows 11 风格的设置界面，跟随系统深浅色；修改后立即生效并自动保存
- `Ctrl + Alt + Space` 全局显示/隐藏快捷键
- 系统托盘菜单与后台驻留
- 自动排除桌面、任务栏、隐藏窗口、幽灵窗口和工具窗口
- 无遥测、无网络通信

## 系统要求

- Windows 10 2004（`10.0.19041`）或更高版本
- 支持 Windows 11
- x64 处理器
- 桌面窗口管理器已启用

StageBar 可以预览管理员权限应用，但 Windows 可能阻止普通权限运行的 StageBar 激活以管理员身份运行的窗口。

## 使用方法

1. 安装并启动 StageBar。
2. 在托盘菜单中打开 **设置**，选择固定或悬浮模式、是否隐藏其他窗口，以及展示宽度和图标大小。
3. 点击卡片，让该应用登上舞台；当前应用会收进条带。
4. 把卡片拖到桌面上，让该应用与当前应用同时显示。
5. 点击桌面壁纸清空舞台；打开或激活任意应用即可开始新的舞台。
6. 悬浮模式下条带被遮挡时，将鼠标移动到显示器最左侧 2 像素区域。

其他操作：

| 操作 | 控制方式 |
| --- | --- |
| 显示或隐藏悬浮面板 | `Ctrl + Alt + Space` |
| 打开托盘菜单 | 右键点击 StageBar 托盘图标 |
| 切换面板显示状态 | 双击托盘图标 |
| 浏览应用卡片 | 鼠标滚轮 |
| 把应用加入舞台 | 将卡片拖到桌面上 |

设置文件保存在 `%LOCALAPPDATA%\StageManager\settings.json`。这里有意保留旧目录名称，以便从早期构建升级时继续使用原有设置。

## 实现原理

| 组件 | 作用 |
| --- | --- |
| WPF | 侧边栏、设置界面、弹簧动画与交互 |
| DWM 缩略图 | 由 Windows 合成的窗口预览 |
| 过渡层 | 无重定向位图的透明穿透窗口，在条带与舞台之间播放缩略图动画 |
| 窗口停放 | 把非活动窗口移到桌面之外、并保持所在显示器的 DPI，附带崩溃恢复记录 |
| WinEvent 钩子 | 无需轮询即可响应激活、新窗口、最小化和关闭 |
| 透明图标覆盖层 | 确保应用图标显示在 DWM 缩略图上方 |
| Windows AppBar API | 固定模式下预留桌面工作区 |
| Win32 窗口目录 | 发现、过滤、还原并激活应用窗口 |

## 从源码构建

需要：

- [.NET 8 SDK](https://dotnet.microsoft.com/download/dotnet/8.0)
- Windows 10/11

```powershell
git clone git@github.com:renkai9418/stage-bar.git
cd stage-bar
dotnet build .\StageManager\StageManager.csproj -c Release
dotnet run --project .\StageManager\StageManager.csproj
```

Debug 构建支持两个开发用环境变量，避免测试时打乱自己的窗口：`STAGEBAR_APP_FILTER`（逗号分隔、需要管理的可执行文件名）和 `STAGEBAR_DATA_DIR`（设置与停放记录所在目录）。

### 生成安装程序

安装 [Inno Setup 6](https://jrsoftware.org/isdl.php)，然后执行：

```powershell
.\installer\build-installer.ps1 -Version 0.0.1
```

脚本会发布自包含的 `win-x64` 单文件程序，并生成：

```text
artifacts\installer\StageBar-Setup-0.0.1-x64.exe
```

## 发布流程

推送版本标签后，会自动触发 Windows Release 工作流：

```powershell
git tag v0.0.1
git push origin v0.0.1
```

工作流会构建安装程序并上传到 GitHub Release。如果存在 `release-notes/<版本>.md`，会将其作为 Release 说明。

## 当前限制

- DRM 保护视频、部分游戏和独占全屏应用可能不提供可用的 DWM 缩略图。
- Windows 权限隔离可能阻止 StageBar 激活管理员权限窗口；普通权限的 StageBar 也无法隐藏这些窗口。
- Windows 不会重绘位于桌面之外的窗口，因此隐藏应用的条带预览会停留在最后一帧，直到它重新登台。
- 置顶窗口不受管理，始终浮在舞台之上。
- 首个预览版本尚未进行代码签名。

## 参与贡献

欢迎提交 Issue 和 Pull Request。修改代码时建议：

1. Fork 仓库并创建目标明确的分支。
2. 保持 DWM 系统合成预览方案，避免截图或目标窗口重绘循环。
3. 使用 Release 配置完成构建，并测试固定和悬浮两种模式。
4. 在 Pull Request 中说明行为变化和手动验证结果。

Bug 反馈和功能建议请提交到 [GitHub Issues](https://github.com/renkai9418/stage-bar/issues)。

## 隐私说明

StageBar 仅在本地处理窗口句柄、标题、图标和 DWM 缩略图，不包含分析统计、遥测、账户系统或网络服务。

## 开源许可证

StageBar 使用 [MIT License](LICENSE) 开源。
