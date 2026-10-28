<p align="center">
  <img src="stage_mgr_go/resources/icon.png" width="128" height="128" alt="Stage Manager for Windows 图标">
</p>

<h1 align="center">Stage Manager for Windows</h1>

<p align="center">
  把 macOS 的“台前调度”搬到 Windows 上。
</p>

<p align="center">
  <strong>简体中文</strong> · <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/e1roy/stage-manager-for-windows/releases"><img src="https://img.shields.io/github/v/release/e1roy/stage-manager-for-windows" alt="最新版本"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/e1roy/stage-manager-for-windows" alt="MIT 许可证"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%2F11-0078D4" alt="Windows 10/11">
</p>

https://github.com/user-attachments/assets/d7c72abf-a951-44b9-9254-7b855a17b556

<p align="center">
  <img src="docs/screenshot.png" width="100%" alt="一个应用在屏幕中央，其余收在左侧">
</p>

## 它是干什么的

桌面上只留你正在用的那个应用，其他的都缩成小卡片排在屏幕左边。想切到哪个，点一下卡片就行。

## 功能

- 每台显示器都有自己的舞台和侧边栏：桌面上只留正在用的应用。
- 其余按应用分组收成卡片，排在屏幕左边或右边（按显示器设置）。
- 点卡片切换，Alt+Tab 和任务栏照常用。
- 把卡片拖到桌面，或者 Shift+点击，就能两个应用并排。
- “所有窗口”概览：点一个窗口切换过去，鼠标移上去可以直接关掉它。
- 把窗口拖到另一台显示器，它就加入那台显示器的舞台。
- 侧边栏可固定或浮动，按显示器设置，宽度、图标、间距都能调。
- 右键卡片：切换过去、加入舞台、拆开组合、关掉窗口。
- 不会丢窗口：退出、崩溃、注销都会把所有窗口放回原处。
- 界面支持简体中文、繁体中文、英文、日文。

## 安装

到 [Releases](https://github.com/e1roy/stage-manager-for-windows/releases/latest) 下载安装包，双击装上就能用，不需要管理员权限。

> [!NOTE]
> 安装包没有签名，Windows 可能提示“未知发布者”，点“仍要运行”即可。

支持 Windows 10 和 11（64 位）。

## 怎么用

- **切换应用**：点左边的卡片，或者照常用 Alt+Tab、任务栏。
- **看全部窗口**：Win+Tab，或者侧边栏底部的面板。点一个窗口切换过去，鼠标移上去可以直接关掉它。
- **两个应用并排**：把卡片拖到桌面，或者按住 Shift 点它。
- **设置**：左键点托盘图标。可以选侧边栏是“固定”（一直占着左边）还是“浮动”（被挡住就藏起来，鼠标移到屏幕最左边再出来），也能调宽度、图标大小、动画速度、开机启动。
- **拆开一组**：右键卡片。
- **退出**：右键托盘图标 → 退出。

## 已知问题

- 卡片里显示的是窗口收起那一刻的截图，不会实时更新。
- 部分游戏、受版权保护的视频截不到图，卡片上只显示图标。
- 以管理员身份运行的程序可能管不了。

## 自己编译

需要 Go 1.27.3 以上：

```powershell
git clone git@github.com:e1roy/stage-manager-for-windows.git
cd stage-manager-for-windows\stage_mgr_go
go run .
```

代码结构、调试方法见 [stage_mgr_go/README.md](stage_mgr_go/README.md)。欢迎提 [Issue](https://github.com/e1roy/stage-manager-for-windows/issues) 和 PR。

## 其他

不联网，不收集任何数据。[MIT 许可证](LICENSE)。

## 致谢

本项目 fork 自 [renkai9418/stage-bar](https://github.com/renkai9418/stage-bar)，感谢 [@renkai9418](https://github.com/renkai9418) 的原始工作。
