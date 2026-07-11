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

StageBar 在桌面左侧展示已打开应用窗口的实时预览。它直接使用 Windows 桌面窗口管理器（DWM）的缩略图接口，因此不需要反复截图或强制目标窗口重绘，也能保持实时画面。

提供两种展示模式：

- **固定模式：** 通过 Windows AppBar API 预留左侧工作区，最大化窗口会使用剩余区域，不会遮挡 StageBar。
- **悬浮模式：** 默认自动隐藏；鼠标到达屏幕左边沿时以动画弹出，离开后自动收回。

## 下载安装

请从 [GitHub Releases](https://github.com/renkai9418/stage-bar/releases/latest) 下载最新版：

- [StageBar Windows x64 安装程序](https://github.com/renkai9418/stage-bar/releases/latest/download/StageBar-Setup-0.0.1-x64.exe)

安装包采用自包含发布，目标电脑不需要单独安装 .NET。默认仅为当前用户安装，不需要管理员权限。

> [!WARNING]
> `0.0.1` 版本尚未进行代码签名，Windows SmartScreen 可能显示“未知发布者”提示。运行前请核对 Release 页面提供的 SHA-256。

## 主要功能

- 常规桌面应用窗口的 DWM 实时预览
- 使用系统合成器提供稳定画面，不使用 `PrintWindow` 重绘循环
- 固定模式通过 AppBar 预留桌面工作区
- 悬浮模式支持左侧边缘唤出、自动隐藏和过渡动画
- 可配置展示宽度与可见卡片数量
- 根据当前显示器高度动态限制最大卡片数
- 鼠标滚轮切换卡片并自动吸附
- 点击窗口时的抽屉式切换动画
- 应用图标独立显示在 DWM 预览上层
- 多显示器定位与 Per-Monitor DPI 感知
- 设置修改后立即生效并自动保存
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
2. 在托盘菜单中打开 **设置**，选择固定或悬浮模式。
3. 悬浮模式下，将鼠标移动到任意显示器最左侧 2 像素区域。
4. 在卡片区域滚动鼠标滚轮浏览窗口。
5. 点击卡片即可还原并切换到目标窗口。

其他操作：

| 操作 | 控制方式 |
| --- | --- |
| 显示或隐藏悬浮面板 | `Ctrl + Alt + Space` |
| 打开托盘菜单 | 右键点击 StageBar 托盘图标 |
| 切换面板显示状态 | 双击托盘图标 |
| 浏览窗口卡片 | 鼠标滚轮 |

设置文件保存在 `%LOCALAPPDATA%\StageManager\settings.json`。这里有意保留旧目录名称，以便从早期构建升级时继续使用原有设置。

## 实现原理

| 组件 | 作用 |
| --- | --- |
| WPF | 侧边栏、设置界面、动画与交互 |
| DWM 缩略图 | 由 Windows 合成的实时窗口预览 |
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
- Windows 权限隔离可能阻止 StageBar 激活管理员权限窗口。
- 当前按单个窗口展示，暂未实现应用分组和工作区功能。
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
