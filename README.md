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

- One stage and one strip per monitor: only the app you're using stays on the desktop.
- The rest are grouped by app into cards, on the left or the right edge.
- Click a card to switch; Alt+Tab and the taskbar work as usual.
- Drag a card to the desktop, or Shift-click it, for two apps side by side.
- Overview of every window on a display: click one to switch, or hover it to close it.
- Drag a window to another monitor and it joins the stage there.
- Drag a card up or down to reorder the strip, or into another monitor's strip to move the app there without opening it.
- Fixed or floating strip, per monitor, with adjustable width, icons and spacing.
- Right-click a card to switch to it, join the stage, split a group, or close its windows.
- Nothing gets lost: quitting, crashing or signing out puts every window back.
- Interface in Simplified Chinese, Traditional Chinese, English and Japanese.

## Install

Grab the installer from [Releases](https://github.com/e1roy/stage-manager-for-windows/releases/latest) and run it. No admin rights needed.

> [!NOTE]
> The installer isn't signed, so Windows may warn about an unknown publisher. Click "Run anyway".

Works on Windows 10 and 11 (64-bit).

## How to use

- **Switch apps**: click a card on the left, or use Alt+Tab and the taskbar as usual.
- **See every window**: Win+Tab, or the panel at the bottom of the strip. Click one to switch to it; hover it to close it.
- **Two apps at once**: drag a card onto the desktop, or Shift-click it.
- **Settings**: left-click the tray icon. Choose whether the strip is Fixed (always takes the left edge) or Floating (hides when covered, comes back when you push the mouse to the left edge), and adjust width, icon size, animation speed and launch at login.
- **Split a group**: right-click its card.
- **Quit**: right-click the tray icon → Quit.

## Known issues

- Cards show a snapshot from the moment the window was tucked away; they don't update live.
- Some games and DRM-protected video can't be captured, so their card shows only the icon.
- Apps running as administrator may not be manageable.

## Build it yourself

Requires Go 1.27.3 or newer:

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
