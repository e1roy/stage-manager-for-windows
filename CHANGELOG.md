# Changelog

All notable changes to StageBar are documented in this file.

## [Unreleased]

### Added

- Stage Manager behavior: only the current app stays on the desktop and other apps are hidden into the strip, with a setting to turn it off.
- Animated hand-over between stage and strip through a click-through DWM transition layer.
- App grouping with stacked previews and drag-to-stage.
- The app leaving the stage takes the slot of the picked card, as on macOS, so the strip keeps its order.
- Event-driven updates through WinEvent hooks for activation, new windows, minimizing, and closing.
- Wallpaper click sends the stage to the strip.
- Crash-recovery journal and automatic restore of hidden windows on exit, crash, and sign-out.
- Icon size setting for the app icons on strip cards.

### Changed

- Strip motion uses interruptible spring animations; the panel slides inside a stationary window.
- Floating mode stays visible while uncovered and steps aside when a stage window overlaps it.
- Previews are cropped to the visible window frame and sized to each window's aspect ratio.
- Strip cards are drawn in perspective like the macOS strip, from vertical DWM thumbnail slices, with soft shadows; windows turn as they fly in and out.
- App icons are larger and drawn without a dark plate, as on macOS.
- Cards shrink smoothly as apps are added so the strip fits the screen height; the visible card count setting is removed.
- StageBar's own windows no longer play DWM show/hide fades, which flashed during transitions.
- Strip cards follow macOS more closely: they share one left edge, every window gets the same height as in Alt+Tab so wide windows are no longer thin strips, a turned window is narrower than when it faces the viewer and widens as hover turns it, and the app icon is smaller and sits on the card's corner.

- An app moving into the strip shows its icon while its windows are still flying in, instead of after they have landed.
- Panel width goes down to 100 px, and the default is 200 px.
- Cards resting in the strip show a still of each window instead of a live preview, so DWM stops redrawing the strip whenever a window in it changes; a still is taken again when its window is resized.
- The settings window looks like Windows 11 Settings: system title bar, Mica backdrop, Fluent controls, and the system's light/dark mode and accent color.

### Fixed

- Windows flashed briefly when moving into the strip and back onto the stage.
- Apps that repaint asynchronously, such as JetBrains IDEs, showed a blank window for a moment after returning from the strip.
- Windows flickered as they landed: flights now cast the same shadow as real windows and strip cards, and the arriving window is activated before it flies, so its title bar no longer changes colour on arrival.
- A one-frame flicker when a window left or landed: the flight and the real window now overlap for a few frames instead of swapping in the same frame.
- Flights ran at about a quarter of the display's refresh rate: WPF only renders at full rate while its own visuals change, and the transition layer moves nothing but DWM thumbnails. Flights are now paced by DWM's composition and follow the display, for example 120 or 160 Hz.
- A window arriving on the stage could show its leftmost sliver stretched across the whole window for its last frames: a slice reused for the flat window kept the source rectangle DWM had been given while the window was turned.
- Switching apps no longer covers whole monitors with the transition layer, which could look like a full-screen app to Windows.
- A window's selection and title bar went inactive while it flew back into the stage: Windows does not paint off-screen windows, so a parked window now goes off screen before the focus changes and keeps its active colours, and the landed window repaints behind the flight instead of after it.
- The flight into the stage showed the parked window's inactive frame: the arriving window is now hidden in place (transparent and click-through) long enough to take the repaint its activation asked for, then parked again with that frame, so the flight and the window that lands show the active state from the first frame.
- The shadow darkened as a window landed and then thinned out: the flights' drawn shadow stayed up over the shadow Windows draws for the landed window, and the two stacked. The flights' shadow is now dropped inside the same composited frame as the hand-over.
- A landed window could sit as its own thumbnail for a third of a second before the real window took over: the hand-over waited for an exact rectangle, which maximized windows never match, and repainted a window that had already been primed. It now waits for the window to be back on the desktop, which takes one frame.
- The shadow still flashed as a window took off and landed. The flights drew a guessed shadow that was darker and wider than the real one, and swapped it for the real one a few frames late, so both showed at once. The flights now draw the two shadows Windows 11 actually uses, fitted to screen captures: a darker one for a window whose frame is active and a lighter one for the rest, including Chromium and Electron apps, which always get the lighter one. A flight's shadow appears in the same frame the real window leaves, and goes in the same frame it lands.
- A window flying back into the strip under the pointer landed as a turned-away card and then snapped to the hovered card. The card's hover kept following the pointer while the card was hidden, while the flight aimed for the card at rest. The pointer now decides the card's hover before the window takes off, the flight lands in that pose, and the card holds it until it shows its windows again.
- Previews and flights were stretched by up to a percent and lost the outer pixels of the window: the thumbnail was cropped to the visible frame although DWM reports the visible frame as the size of a thumbnail already, so the crop cut into the content. Thumbnails are now drawn whole, which puts back the window controls that were clipped at the edge and stops the width from jumping when a flight lands.
- A window growing out of the strip slid a few pixels to the left just before it landed. While turned, it is drawn from slices whose source rectangles DWM measures from the window's corner rather than from its visible frame, so the picture sat right of where it belonged by the invisible resize border, about 11 px, until it turned flat. Slices are now moved by that border.
- Flights fell well short of the display's refresh rate, about 40 fps at 120 Hz when switching apps: DWM pays for every thumbnail whose size changes in a frame, and a flight resized 24 slices and 44 shadow pieces per window on every frame. Windows and shadows now use only as many slices and segments as their slant needs, and are rounded so that moving one keeps its size; each shadow corner is rounded from its own position, and the shadow's blur spread changes in small steps, so corners only move between steps. The frame rate each flight reached, and what limited it, is written to the transition log.

## [0.0.1] - 2026-07-11

### Added

- Stable live window previews using the Windows DWM thumbnail API.
- Fixed AppBar and animated floating display modes.
- Configurable panel width and visible card count with height-aware limits.
- Mouse-wheel navigation, snapping, and drawer-style window activation.
- Application badges rendered above DWM thumbnails through a transparent overlay.
- Live settings, multi-monitor positioning, global hotkey, and system tray controls.
- StageBar branding, application icon, self-contained installer, and bilingual documentation.

[0.0.1]: https://github.com/renkai9418/stage-bar/releases/tag/v0.0.1
