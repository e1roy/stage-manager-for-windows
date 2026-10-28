<p align="center">
  <img src="stage_mgr_go/resources/icon.png" width="128" height="128" alt="Stage Manager for Windows icon">
</p>

<h1 align="center">Stage Manager for Windows</h1>

<p align="center">
  macOS Stage Manager, on Windows.
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a> · <strong>English</strong>
</p>

<p align="center">
  <a href="https://github.com/e1roy/stage-manager-for-windows/releases"><img src="https://img.shields.io/github/v/release/e1roy/stage-manager-for-windows" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/e1roy/stage-manager-for-windows" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%2F11-0078D4" alt="Windows 10/11">
</p>

https://github.com/user-attachments/assets/d7c72abf-a951-44b9-9254-7b855a17b556

<p align="center">
  <img src="docs/screenshot.png" width="100%" alt="One app in the middle of the screen, the rest tucked away on the left">
</p>

## What it does

Only the app you're using stays on the desktop. Everything else shrinks into cards along the left edge. Click a card to switch to it.

## Features

- **A stage and a strip for every monitor.** The app you're using stays in the middle; the rest are grouped by app into cards along the left edge — or the right, chosen per monitor. Windows never move between monitors, so their DPI never changes.
- **Switching.** Click a card and the two apps trade places. Alt+Tab, the taskbar, and a window you just opened take the stage at once. Click the wallpaper to send the current app away too (can be turned off).
- **Two apps side by side.** Drag a card onto the desktop, or Shift-click it, to add its app to the stage. Right-click a card to switch to it, join it to the stage, split a group, or close its windows.
- **An overview of every window**, Mission Control's way: the windows of one display spread out over the dimmed desktop, with the stage each belongs to. Click one to switch to it, or hover it and click the close button in its corner to close it. Click anywhere else or press Escape to send them all back. Open it from the panel at the bottom of the strip, from the button over the display's bottom-left corner (where a centered Windows 11 taskbar leaves that corner empty), or with Win+Tab, which closes it again.
- **Dragging windows.** Drag a window onto another monitor and the stage there makes way for it; drag it into the strip to take its app off the stage. A window moved with Win+Shift+Arrow takes the stage as a dropped one does.
- **Nothing gets lost.** Apps off the stage are parked just beyond the desktop rather than minimized, so they keep their size and state and come back at once. Every move is logged, so quitting, crashing, being killed, or signing out puts every window back. Windows you peek at from the taskbar come back while you look.
- **Fixed or floating strip.** Fixed reserves the edge of the screen, as the taskbar does, and yields to apps going full screen. Floating shows while there is room, slides away when a stage window covers it, and comes back as the pointer reaches the edge.
- **Settings**, from the tray icon: strip mode, width, icon size, spacing, how far the cards turn in 3D, image quality, hiding apps off the stage, clicking the wallpaper, each monitor's strip side and whether it has one at all, companion apps that join the stage instead of taking it (a commit dialog beside the file manager it came from), the size of the overview's panel and corner button, Win+Tab, launch at login, and the color the overview's buttons take under the pointer. The interface speaks Simplified Chinese, Traditional Chinese, English and Japanese, following Windows unless you pick one.
- **Cards stack an app's windows**, up to three deep; as apps pile up the cards shrink, and then scroll.
- **Tray icon**: left-click opens settings; the right-click menu brings back any window left off screen, opens settings, or quits. `StageManagerForWindows.exe --quit` quits a running instance and brings every window back, as the installer does.
- No network access, no data collection.

## Install

Grab the installer from [Releases](https://github.com/e1roy/stage-manager-for-windows/releases/latest) and run it. No admin rights needed.

> [!NOTE]
> The installer isn't signed, so Windows may warn about an unknown publisher. Click "Run anyway".

Works on Windows 10 and 11 (64-bit).

## How to use

- **Switch apps**: click a card on the left, or use Alt+Tab and the taskbar as usual.
- **See everything**: open the overview from the panel at the bottom of the strip, from the button over the display's bottom-left corner, or with Win+Tab. Click a window to switch to it; hover it and click the button in its top-right corner to close it.
- **Two apps at once**: drag a card onto the desktop, or Shift-click it.
- **Settings**: left-click the tray icon. Choose whether the strip is Fixed (always takes the left edge) or Floating (hides when covered, comes back when you push the mouse to the left edge), and adjust width, icon size, animation speed and launch at login.
- **Split a group**: right-click its card.
- **Quit**: right-click the tray icon → Quit.

## Known issues

- Cards show a snapshot from the moment the window was tucked away; they don't update live.
- Some games and DRM-protected video can't be captured, so their card shows only the icon.
- Apps running as administrator may not be manageable.

## Build it yourself

Requires Go 1.27.1 or newer:

```powershell
git clone git@github.com:e1roy/stage-manager-for-windows.git
cd stage-manager-for-windows\stage_mgr_go
go run .
```

See [stage_mgr_go/README.md](stage_mgr_go/README.md) for the code layout and debugging tips. [Issues](https://github.com/e1roy/stage-manager-for-windows/issues) and PRs welcome.

## Other

No network access, no data collection. [MIT License](LICENSE).

## Acknowledgements

This project is forked from [renkai9418/stage-bar](https://github.com/renkai9418/stage-bar). Thanks to [@renkai9418](https://github.com/renkai9418) for the original work.
