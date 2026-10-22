using System.Drawing;
using System.Windows.Forms;
using System.Windows.Threading;
using StageManager.Controls;
using StageManager.Interop;
using StageManager.Models;
using StageManager.Services;
using FormsCursor = System.Windows.Forms.Cursor;
using MouseButtonEventArgs = System.Windows.Input.MouseButtonEventArgs;
using MouseEventArgs = System.Windows.Input.MouseEventArgs;

namespace StageManager;

/// <summary>
/// The stage model: which applications are on the desktop, which wait in the
/// strip, and the animated hand-over between the two.
/// </summary>
public partial class MainWindow
{
    private static readonly TimeSpan FlightDuration = TimeSpan.FromMilliseconds(520);
    private static readonly TimeSpan ReturnDuration = TimeSpan.FromMilliseconds(320);
    private static readonly TimeSpan StageSettleTime = TimeSpan.FromSeconds(1);
    private const int DragThreshold = 6;
    // How far into a flight the icons of the cards it lands in start fading in.
    private const double IconRevealPoint = 0.3;
    // Composition frames during which the flight and the real content overlap.
    // They show identical pixels, so the overlap is invisible; swapping them in
    // the same frame is not, because DWM can present a moved window a frame late.
    private const int TakeOffFrames = 1;
    private const int LandingFrames = 3;
    // The flights hold their last frame until the real windows are home; anything
    // longer than a few frames of that hold reads as the window hanging before it
    // snaps into place.
    private const int ArrivalTimeout = 120;
    private const int OffscreenTimeout = 50;
    private const int MaximumForegroundAttempts = 6;

    private readonly HashSet<string> _stageKeys = new(StringComparer.Ordinal);
    /// <summary>Strip order of the apps that are not on the stage, top first.</summary>
    private readonly List<string> _stripOrder = [];
    private readonly DispatcherTimer _eventRefreshTimer = new() { Interval = TimeSpan.FromMilliseconds(60) };
    private readonly DispatcherTimer _coverTimer = new() { Interval = TimeSpan.FromMilliseconds(50) };
    private readonly DispatcherTimer _foregroundTimer = new() { Interval = TimeSpan.FromMilliseconds(70) };
    private IReadOnlyList<AppGroup> _groups = [];
    private bool _isTransitioning;
    private bool _isStageInitialized;
    private bool _isPanelCovered;
    private bool _isHoldingPanelOpen;
    private nint _pendingForeground;
    private int _foregroundAttempts;
    private DateTime _stageChangedAt = DateTime.MinValue;
    private CardDrag? _drag;

    private async void InitializeStage()
    {
        // Let the AppBar reservation re-maximize windows before measuring them.
        await Task.Delay(350);
        var groups = BuildGroups(_windowCatalog.GetWindows());
        var foreground = WindowCatalog.GetRootOwner(NativeMethods.GetForegroundWindow());
        var stage = groups.FirstOrDefault(group => group.Windows.Any(window => window.Handle == foreground && IsOnStage(window))) ??
                    groups.FirstOrDefault(group => group.Windows.Any(IsOnStage));

        _stageKeys.Clear();
        if (stage is not null)
        {
            _stageKeys.Add(stage.Key);
        }

        _stageChangedAt = DateTime.UtcNow;
        _isStageInitialized = true;
        if (_settings.HideInactiveWindows)
        {
            ParkInactiveWindows(groups);
        }

        RefreshWindows();
    }

    private void RefreshWindows()
    {
        _eventRefreshTimer.Stop();
        if (!_isStageInitialized || _isTransitioning || _drag is not null || _windowHandle == 0 || _isExiting)
        {
            return;
        }

        ReparkEscapedWindows();
        var groups = BuildGroups(_windowCatalog.GetWindows());
        PruneStage(groups);
        _groups = groups;
        UpdatePanelCovered();
        SyncStrip(GetStripGroups(groups));

        // Refreshes never run during a transition, so every card here rests in
        // the strip and can show stills instead of live windows.
        foreach (var card in _cards)
        {
            card.Freeze();
        }
    }

    /// <summary>
    /// Groups windows by application. Only windows on this display take part,
    /// plus every hidden window, because the strip is the way back to them.
    /// </summary>
    private List<AppGroup> BuildGroups(IReadOnlyList<WindowSnapshot> windows)
    {
        var groups = new Dictionary<string, List<WindowSnapshot>>(StringComparer.Ordinal);
        var order = new List<string>();
        foreach (var window in windows)
        {
            if (!IsHidden(window) && !IsOnStageScreen(window))
            {
                continue;
            }

            if (!groups.TryGetValue(window.AppKey, out var list))
            {
                list = [];
                groups.Add(window.AppKey, list);
                order.Add(window.AppKey);
            }

            list.Add(window);
        }

        return order.Select(key => new AppGroup(key, groups[key])).ToList();
    }

    /// <summary>
    /// The strip in its current order. Apps that join it without taking a
    /// card's place, such as a minimized stage app, enter at the top; the very
    /// first strip follows the windows' z-order.
    /// </summary>
    private IReadOnlyList<AppGroup> GetStripGroups(IReadOnlyList<AppGroup> groups)
    {
        var strip = groups.Where(group => !_stageKeys.Contains(group.Key)).ToArray();
        var keys = strip.Select(static group => group.Key).ToHashSet(StringComparer.Ordinal);
        _stripOrder.RemoveAll(key => !keys.Contains(key));
        var newcomers = strip.Select(static group => group.Key).Where(key => !_stripOrder.Contains(key)).ToArray();
        _stripOrder.InsertRange(0, newcomers);
        return strip.OrderBy(group => _stripOrder.IndexOf(group.Key)).ToArray();
    }

    /// <summary>An app leaves the stage once none of its windows remain visible, e.g. after minimizing.</summary>
    private void PruneStage(IReadOnlyList<AppGroup> groups)
    {
        if (DateTime.UtcNow - _stageChangedAt < StageSettleTime)
        {
            return;
        }

        var visible = groups
            .Where(group => group.Windows.Any(IsOnStage))
            .Select(static group => group.Key)
            .ToHashSet(StringComparer.Ordinal);
        _stageKeys.RemoveWhere(key => !visible.Contains(key));
    }

    private void ParkInactiveWindows(IEnumerable<AppGroup> groups)
    {
        foreach (var window in groups
                     .Where(group => !_stageKeys.Contains(group.Key))
                     .SelectMany(static group => group.Windows)
                     .Where(IsOnStage))
        {
            _parking.Park(window.Handle);
        }
    }

    private void ReparkEscapedWindows()
    {
        var foreground = NativeMethods.GetForegroundWindow();
        foreach (var window in _parking.CollectEscaped())
        {
            var group = _groups.FirstOrDefault(group => group.Windows.Any(snapshot => snapshot.Handle == window));
            if (_settings.HideInactiveWindows &&
                window != foreground &&
                group is not null &&
                !_stageKeys.Contains(group.Key))
            {
                _parking.Park(window);
            }
        }
    }

    private bool IsHidden(WindowSnapshot window) => window.IsMinimized || _parking.IsParked(window.Handle);

    private bool IsOnStageScreen(WindowSnapshot window) =>
        _currentScreen is not null &&
        Screen.FromRectangle(window.FrameBounds).DeviceName == _currentScreen.DeviceName;

    private bool IsOnStage(WindowSnapshot window) => !IsHidden(window) && IsOnStageScreen(window);

    private void UpdatePanelCovered()
    {
        _coverTimer.Stop();
        // Only the band the cards occupy counts, so a window low on the screen
        // does not chase away a short strip.
        var panelBounds = GetCardsScreenBounds();
        if (panelBounds.IsEmpty)
        {
            _isPanelCovered = false;
            return;
        }

        _isPanelCovered = _groups
            .SelectMany(static group => group.Windows)
            .Where(IsOnStage)
            .Any(window => NativeMethods.IsWindow(window.Handle) &&
                           !NativeMethods.IsIconic(window.Handle) &&
                           WindowGeometry.GetFrameBounds(window.Handle).IntersectsWith(panelBounds));
    }

    private void WinEvents_WindowChanged(nint window, uint eventType)
    {
        if (eventType == NativeMethods.EventObjectLocationChange)
        {
            // Our own parking moves are not interesting; user moves only
            // matter for whether the floating strip is covered.
            if (!_parking.IsParked(window) && _settings.DisplayMode is DisplayMode.Floating && !_coverTimer.IsEnabled)
            {
                _coverTimer.Start();
            }

            return;
        }

        if (!_eventRefreshTimer.IsEnabled)
        {
            _eventRefreshTimer.Start();
        }
    }

    private void WinEvents_ForegroundChanged(nint window)
    {
        if (!_isStageInitialized)
        {
            return;
        }

        if (WindowCatalog.IsDesktop(window))
        {
            // Clicking the wallpaper sends the stage to the strip, as on macOS.
            // The button check keeps Win+D and similar shortcuts out of it.
            if (ShouldRevealDesktop())
            {
                _ = SwitchStageAsync([], 0, activate: false);
            }

            return;
        }

        _pendingForeground = window;
        _foregroundAttempts = 0;
        _foregroundTimer.Stop();
        _foregroundTimer.Start();
    }

    private bool ShouldRevealDesktop()
    {
        var button = SystemInformation.MouseButtonsSwapped ? NativeMethods.VkRButton : NativeMethods.VkLButton;
        var cursor = FormsCursor.Position;
        return _settings.HideInactiveWindows &&
               !_isTransitioning &&
               _stageKeys.Count > 0 &&
               (NativeMethods.GetAsyncKeyState(button) & 0x8000) != 0 &&
               Screen.FromPoint(cursor).DeviceName == _currentScreen?.DeviceName &&
               !IsCursorInsideWindow(cursor);
    }

    private async void ForegroundTimer_Tick(object? sender, EventArgs e)
    {
        _foregroundTimer.Stop();
        var window = _pendingForeground;
        if (window == 0 || _isTransitioning || _drag is not null)
        {
            return;
        }

        var root = WindowCatalog.GetRootOwner(window);
        var snapshot = _windowCatalog.GetWindows().FirstOrDefault(candidate => candidate.Handle == root);
        if (snapshot is null)
        {
            // New windows often get their title or become uncloaked a moment
            // after they are activated.
            if (++_foregroundAttempts < MaximumForegroundAttempts && NativeMethods.IsWindow(window))
            {
                _foregroundTimer.Start();
            }
            else
            {
                _pendingForeground = 0;
            }

            return;
        }

        _pendingForeground = 0;
        if (!IsHidden(snapshot) && !IsOnStageScreen(snapshot))
        {
            return;
        }

        if (_stageKeys.Contains(snapshot.AppKey))
        {
            if (_parking.IsParked(snapshot.Handle))
            {
                _parking.Unpark(snapshot.Handle);
            }

            return;
        }

        await SwitchStageAsync([snapshot.AppKey], snapshot.Handle, activate: false);
    }

    /// <summary>
    /// Makes <paramref name="stageKeys"/> the applications on the stage. Windows
    /// leaving the stage shrink into their strip cards while the arriving
    /// windows grow out of theirs, then the real windows are swapped in place.
    /// </summary>
    private async Task SwitchStageAsync(
        IReadOnlyCollection<string> stageKeys,
        nint focusWindow,
        bool activate,
        IReadOnlyDictionary<nint, FlightPose>? origins = null)
    {
        if (_isTransitioning || _transition is null)
        {
            return;
        }

        _isTransitioning = true;
        var landingCards = new List<WindowPreviewControl>();
        var restored = new List<nint>();
        var unanimated = new List<nint>();
        try
        {
            var groups = BuildGroups(_windowCatalog.GetWindows());
            var newStage = stageKeys.ToHashSet(StringComparer.Ordinal);
            var vanishingY = GetVanishingScreenY();
            var inactiveShadow = GetWindowShadow(isActive: false);
            var activeShadow = GetWindowShadow(isActive: true);
            var cardShadow = GetCardShadow();
            var departing = _settings.HideInactiveWindows
                ? groups.Where(group => !newStage.Contains(group.Key) && group.Windows.Any(IsOnStage)).ToList()
                : [];
            var arriving = groups.Where(group => newStage.Contains(group.Key)).ToList();
            TransitionTrace.Line($"switch begin stage=[{string.Join('|', stageKeys)}] activate={activate} " +
                                 $"focus=0x{focusWindow:X} scale={GetDeviceScale():F2}");

            // Arriving windows launch from where the user sees them.
            var arrivingFlights = new List<FlightSpec>();
            foreach (var group in arriving)
            {
                var card = _cards.FirstOrDefault(candidate => candidate.Key == group.Key);
                foreach (var window in group.Windows.Where(IsHidden).Reverse())
                {
                    var home = GetHomeFrameBounds(window);
                    if (home.Width <= 0 || home.Height <= 0)
                    {
                        continue;
                    }

                    restored.Add(window.Handle);
                    TransitionTrace.Line($"arrive 0x{window.Handle:X} bounds={window.Bounds} frame={window.FrameBounds} " +
                               $"home={home} client={DescribeClient(window.Handle)} iconic={window.IsMinimized} title='{window.Title}'");
                    if (origins?.TryGetValue(window.Handle, out var origin) == true)
                    {
                        arrivingFlights.Add(new FlightSpec(
                            window.Handle,
                            origin.Rect,
                            home,
                            window.Crop,
                            FromTilt: origin.Tilt,
                            VanishingY: vanishingY,
                            FromShadow: origin.Shadow,
                            ToShadow: inactiveShadow,
                            ToActiveShadow: activeShadow));
                    }
                    else if (card is not null && IsCardOnScreen(card) && GetCardScreenRect(card, window.Handle) is { } from)
                    {
                        // The window turns to face the viewer as it grows out of its card.
                        arrivingFlights.Add(new FlightSpec(
                            window.Handle,
                            from,
                            home,
                            window.Crop,
                            FromTilt: (float)card.Tilt,
                            VanishingY: vanishingY,
                            FromShadow: cardShadow,
                            ToShadow: inactiveShadow,
                            ToActiveShadow: activeShadow));
                    }
                    else
                    {
                        arrivingFlights.Add(new FlightSpec(
                            window.Handle,
                            Shrink(home, 0.94f),
                            home,
                            window.Crop,
                            0,
                            1,
                            FromShadow: inactiveShadow,
                            ToShadow: inactiveShadow,
                            ToActiveShadow: activeShadow));
                    }
                }
            }

            // As on macOS, the apps leaving the stage take the place of the card
            // that was picked, so the rest of the strip stays where it was. With
            // no card to replace they enter at the top.
            var slot = _stripOrder.FindIndex(key => newStage.Contains(key));
            _stripOrder.RemoveAll(key => newStage.Contains(key) || departing.Any(group => group.Key == key));
            slot = Math.Clamp(slot, 0, _stripOrder.Count);
            _stripOrder.InsertRange(slot, departing.Select(static group => group.Key));
            if (departing.Count > 0)
            {
                _scrollIndex = Math.Min(_scrollIndex, slot);
            }

            _stageKeys.Clear();
            _stageKeys.UnionWith(newStage);
            _stageChangedAt = DateTime.UtcNow;
            _groups = groups;
            var departingKeys = departing.Select(static group => group.Key).ToHashSet(StringComparer.Ordinal);
            var arrivingCards = _cards.Where(card => newStage.Contains(card.Key)).ToArray();
            SyncStrip(GetStripGroups(groups), departingKeys);

            var departingFlights = new List<FlightSpec>();
            foreach (var group in departing)
            {
                var card = _cards.First(candidate => candidate.Key == group.Key);
                card.IsInFlight = true;
                SettleHover(card);
                landingCards.Add(card);
                var frontRect = GetCardTargetScreenRect(card, group.Front.Handle);
                foreach (var window in group.Windows.Where(IsOnStage).Reverse())
                {
                    TransitionTrace.Line($"depart 0x{window.Handle:X} bounds={window.Bounds} frame={window.FrameBounds} " +
                                         $"client={DescribeClient(window.Handle)}");
                    var stacked = card.GetLayers().Any(layer => layer.Handle == window.Handle);
                    var target = GetCardTargetScreenRect(card, window.Handle) ?? frontRect;
                    var windowShadow = WindowCatalog.HasActiveFrame(window.Handle) ? activeShadow : inactiveShadow;
                    departingFlights.Add(target is { } to
                        ? new FlightSpec(
                            window.Handle,
                            window.FrameBounds,
                            to,
                            window.Crop,
                            1,
                            stacked ? 1 : 0,
                            ToTilt: (float)card.Tilt,
                            VanishingY: vanishingY,
                            FromShadow: windowShadow,
                            ToShadow: cardShadow,
                            HoldsShadow: true)
                        : new FlightSpec(
                            window.Handle,
                            window.FrameBounds,
                            CollapseToEdge(window.FrameBounds),
                            window.Crop,
                            1,
                            0,
                            FromShadow: windowShadow,
                            ToShadow: windowShadow,
                            HoldsShadow: true));
                }
            }

            if (departingFlights.Count + arrivingFlights.Count == 0)
            {
                if (activate)
                {
                    ActivateFocus(focusWindow, arriving);
                }

                return;
            }

            if (departingFlights.Count > 0)
            {
                // Reveal a hidden floating strip so windows visibly land in it.
                _isHoldingPanelOpen = true;
                ShowPanel();
            }

            // Like Windows, activate before the window flies rather than after
            // it lands: otherwise its title bar visibly changes to the active
            // colour on arrival. A minimized one would restore with the system
            // animation, so it still waits until it has been restored without one.
            var focusTarget = activate ? GetFocusTarget(focusWindow, arriving) : 0;
            var isActivatedEarly = focusTarget != 0 && !NativeMethods.IsIconic(focusTarget);

            // The arriving window is registered last so it flies above the rest.
            _transition.Begin([.. departingFlights, .. arrivingFlights]);
            foreach (var card in arrivingCards)
            {
                card.IsInFlight = true;
            }

            ApplyStripVisuals();

            // Only hide the real windows once the flights are certainly on screen.
            await WaitForCompositionAsync(TakeOffFrames);

            // The departing windows take their own shadows with them, so the
            // flights' shadows appear the instant they are gone.
            var transition = _transition;
            var departingWindows = departingFlights.Select(static flight => flight.Source).ToArray();
            var takeOff = WatchFor(() => !departingWindows.Any(IsOnScreen), OffscreenTimeout, transition.RevealShadows);
            foreach (var flight in departingFlights)
            {
                _parking.Park(flight.Source);
            }

            // Windows does not paint a window that is off the desktop, so from
            // here on the flights show the frame that was frozen when the window
            // was parked. Park before handing focus over, so a departing window
            // keeps its active colours: that is the frame the flight back into
            // the stage needs, and what Windows' own minimize animation shows.
            await takeOff;
            if (isActivatedEarly)
            {
                var isActivated = WindowCatalog.Activate(focusTarget);
                TransitionTrace.Line($"activate 0x{focusTarget:X} granted={isActivated} " +
                                     $"foreground=0x{NativeMethods.GetForegroundWindow():X}");
            }

            var flying = _transition.AnimateAsync(FlightDuration);
            if (landingCards.Count > 0)
            {
                // The windows travel most of the way early in the flight; bring
                // their icons in now so they are in place when the windows land
                // rather than after the hand-over.
                await Task.Delay(FlightDuration * IconRevealPoint);
                _iconOverlay?.BringToTop();
                foreach (var card in landingCards)
                {
                    card.RevealIcon();
                }

                RequestFrame();
            }

            await flying;

            // A minimized window can only be brought back by restoring it, and
            // activating it is the only way to restore it without the system
            // zoom; the other windows of its app are restored silently.
            var activationRestores = activate && !isActivatedEarly ? focusTarget : 0;

            // The landed windows cast their own shadows from the first frame they
            // are home, so the flights' shadows go the instant they get there.
            var landing = WatchFor(() => restored.All(HasArrived), ArrivalTimeout, () => transition.CutShadows(restored));
            foreach (var handle in restored)
            {
                // A window that comes back on screen from outside it is faded in by
                // DWM, which reads as the window blinking out and back. The flight
                // covers the hand-over, so the animation is only in the way.
                unanimated.Add(handle);
                WindowCatalog.SetTransitionsDisabled(handle, true);
                if (_parking.IsParked(handle))
                {
                    _parking.Unpark(handle);
                }
                else if (NativeMethods.IsIconic(handle) && handle != activationRestores)
                {
                    WindowCatalog.RestoreWithoutAnimation(handle);
                }
            }

            if (activate && !isActivatedEarly)
            {
                // Activating a minimized window restores it; with its transitions
                // disabled it lands silently under the flight instead of playing
                // the system zoom on top of it.
                foreach (var handle in restored.Where(NativeMethods.IsIconic))
                {
                    WindowCatalog.SetTransitionsDisabled(handle, true);
                }

                ActivateFocus(focusWindow, arriving);
            }

            // Put the real content back underneath the flights first, and only
            // drop the flights once DWM has had time to present it.
            await landing;
            foreach (var handle in restored)
            {
                // Whatever the flight drew, this is where the window really is;
                // the two disagreeing by a few pixels is the jump on landing.
                TransitionTrace.Line($"land 0x{handle:X} bounds={WindowGeometry.GetBounds(handle)} " +
                           $"frame={WindowGeometry.GetFrameBounds(handle)} client={DescribeClient(handle)} " +
                           $"foreground=0x{NativeMethods.GetForegroundWindow():X}");
            }

            // The window is home as far as the window manager is concerned, but DWM
            // needs a composited frame before it shows up there, and the application
            // then paints whatever the move invalidated — for a Java IDE that is a
            // couple of hundred milliseconds of half-drawn window. Dropping the
            // flights on the first of those frames is the flash, so wait it out
            // underneath them.
            await WaitForCompositionAsync(LandingFrames);

            // Moving a window back on screen does not damage it, so its surface still
            // holds the frame it was parked with. Asking it to repaint here is what
            // makes a slow application put half-drawn frames under the flights.

            // The flights that landed in the strip hand their shadows over to the cards.
            await _transition.FadeOutShadowsAsync(1);
            foreach (var card in landingCards)
            {
                card.IsInFlight = false;
            }

            ApplyStripVisuals();
            _transition.End();
        }
        finally
        {
            foreach (var card in landingCards)
            {
                card.IsInFlight = false;
            }

            // The cards that held their hover through the flight catch up with the pointer.
            RequestFrame();
            _transition?.End();
            // A window left hidden by a failed transition would be invisible and
            // click-through until the process exits; never let one outlive a switch.
            WindowPriming.RevealAll();
            TransitionTrace.Line("switch end");
            _isTransitioning = false;
            if (_isHoldingPanelOpen)
            {
                // Let the user see where the windows went before the strip steps aside.
                _isHoldingPanelOpen = false;
                _lastInsideAt = DateTime.UtcNow;
            }

            ReenableTransitionsLater(unanimated);
            RefreshWindows();
            if (_pendingForeground != 0)
            {
                _foregroundTimer.Start();
            }
        }
    }

    private Rectangle GetHomeFrameBounds(WindowSnapshot window)
    {
        if (!_parking.TryGetHome(window.Handle, out var home))
        {
            // Minimized windows already report the frame they restore to.
            return window.FrameBounds;
        }

        return new Rectangle(
            home.X + window.FrameBounds.X - window.Bounds.X,
            home.Y + window.FrameBounds.Y - window.Bounds.Y,
            window.FrameBounds.Width,
            window.FrameBounds.Height);
    }

    private static string DescribeClient(nint window) =>
        NativeMethods.GetClientRect(window, out var client) ? $"{client.Width}x{client.Height}" : "?";

    private static nint GetFocusTarget(nint focusWindow, IReadOnlyList<AppGroup> arriving) =>
        focusWindow != 0 ? focusWindow : arriving.FirstOrDefault()?.Front.Handle ?? 0;

    private static void ActivateFocus(nint focusWindow, IReadOnlyList<AppGroup> arriving)
    {
        var target = GetFocusTarget(focusWindow, arriving);
        if (target != 0)
        {
            WindowCatalog.Activate(target);
        }
    }

    /// <summary>
    /// Busy-waits on a worker for a posted move to take effect: a window leaving
    /// the desktop before it is activated elsewhere, or coming back to it. Parking
    /// moves are posted, so only the window's own thread knows when they happen.
    /// A timer-based poll notices them up to a timer tick (about 15 ms) late, which
    /// is two frames of both the real shadow and the flight's showing.
    /// </summary>
    private static void SpinUntil(Func<bool> condition, int milliseconds)
    {
        var deadline = Environment.TickCount64 + milliseconds;
        while (!condition() && Environment.TickCount64 < deadline)
        {
            Thread.Yield();
        }
    }

    /// <summary>
    /// Runs <paramref name="action"/> on a worker the moment <paramref name="condition"/>
    /// holds, or once <paramref name="milliseconds"/> have passed. The worker is
    /// already watching when this returns: a window can carry out a posted move
    /// before the call that posted it even returns, and parking then writes its
    /// journal, so a watcher started afterwards can see the move a frame late.
    /// </summary>
    private static Task WatchFor(Func<bool> condition, int milliseconds, Action action)
    {
        var watching = new TaskCompletionSource();
        var watcher = Task.Run(() =>
        {
            watching.SetResult();
            SpinUntil(condition, milliseconds);
            action();
        });
        watching.Task.Wait();
        return watcher;
    }

    /// <summary>
    /// Asks the window manager, which knows of a move the moment the window's
    /// thread makes it. DWM's frame bounds only follow once DWM has taken the move
    /// in, by which time it may already be composing the frame that shows it.
    /// </summary>
    private static bool IsOnScreen(nint window) =>
        NativeMethods.IsWindow(window) &&
        Screen.AllScreens.Any(screen => screen.Bounds.IntersectsWith(WindowGeometry.GetBounds(window)));

    /// <summary>
    /// Waits until the windows have painted the repaint they were asked for, so
    /// the flights can be dropped on top of an up-to-date frame.
    /// </summary>
    private static async Task WaitForRepaintAsync(IReadOnlyList<nint> windows, int milliseconds = 60)
    {
        if (windows.Count == 0)
        {
            return;
        }

        var deadline = Environment.TickCount64 + milliseconds;
        while (Environment.TickCount64 < deadline && windows.Any(HasPendingPaint))
        {
            await Task.Delay(4);
        }
    }

    private static bool HasPendingPaint(nint window)
    {
        if (!NativeMethods.IsWindow(window) || NativeMethods.IsIconic(window))
        {
            return false;
        }

        if (NativeMethods.GetUpdateRect(window, out _, false))
        {
            return true;
        }

        var pending = false;
        NativeMethods.EnumChildWindows(window, (child, parameter) =>
        {
            if (NativeMethods.GetUpdateRect(child, out _, false))
            {
                pending = true;
                return false;
            }

            return true;
        }, 0);
        return pending;
    }

    /// <summary>Lets DWM present <paramref name="frames"/> frames without blocking the UI thread.</summary>
    private static Task WaitForCompositionAsync(int frames) => Task.Run(() =>
    {
        for (var frame = 0; frame < frames; frame++)
        {
            NativeMethods.DwmFlush();
        }
    });

    /// <summary>
    /// A window that still reads as off screen has not moved yet. Asking for its
    /// exact rectangle instead misses windows that clamp their own size, such as
    /// maximized ones, and holds the last flight frame for nothing.
    /// </summary>
    private static bool HasArrived(nint window)
    {
        if (!NativeMethods.IsWindow(window))
        {
            return true;
        }

        return !NativeMethods.IsIconic(window) && IsOnScreen(window);
    }

    private void ReenableTransitionsLater(IReadOnlyList<nint> windows)
    {
        if (windows.Count == 0)
        {
            return;
        }

        var handles = windows.ToArray();
        var timer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(600) };
        timer.Tick += (_, _) =>
        {
            timer.Stop();
            foreach (var handle in handles.Where(NativeMethods.IsWindow))
            {
                WindowCatalog.SetTransitionsDisabled(handle, false);
            }
        };
        timer.Start();
    }

    private RectangleF CollapseToEdge(Rectangle bounds)
    {
        var screen = _currentScreen?.WorkingArea ?? bounds;
        var width = bounds.Width / 6f;
        var height = bounds.Height / 6f;
        return new RectangleF(screen.Left, screen.Top + (screen.Height - height) / 2, width, height);
    }

    private static RectangleF Shrink(Rectangle bounds, float scale)
    {
        var width = bounds.Width * scale;
        var height = bounds.Height * scale;
        return new RectangleF(
            bounds.Left + (bounds.Width - width) / 2,
            bounds.Top + (bounds.Height - height) / 2,
            width,
            height);
    }

    private void Card_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        if (_isTransitioning || _drag is not null || sender is not WindowPreviewControl { IsRemoving: false } card)
        {
            return;
        }

        e.Handled = true;
        _drag = new CardDrag(card, FormsCursor.Position);
        card.Press.Target = 1;
        card.CaptureMouse();
        RequestFrame();
    }

    private void Card_MouseMove(object sender, MouseEventArgs e)
    {
        if (_drag is not { } drag || !ReferenceEquals(drag.Card, sender) || _transition is null)
        {
            return;
        }

        var cursor = FormsCursor.Position;
        var offsetX = cursor.X - drag.Start.X;
        var offsetY = cursor.Y - drag.Start.Y;
        if (!drag.IsDragging)
        {
            if (offsetX * offsetX + offsetY * offsetY < DragThreshold * DragThreshold)
            {
                return;
            }

            // Hand the card's windows to the transition layer so they can be
            // dragged anywhere on the desktop, beyond the strip window.
            var cardShadow = GetCardShadow();
            var flights = drag.Card.GetLayers()
                .Reverse()
                .Select(layer => GetCardScreenRect(drag.Card, layer.Handle) is { } rect
                    ? new FlightSpec(
                        layer.Handle,
                        rect,
                        rect,
                        layer.Crop,
                        FromTilt: (float)drag.Card.Tilt,
                        ToTilt: (float)drag.Card.Tilt,
                        VanishingY: GetVanishingScreenY(),
                        FromShadow: cardShadow,
                        ToShadow: cardShadow)
                    : (FlightSpec?)null)
                .OfType<FlightSpec>()
                .ToArray();
            _transition.Begin(flights, Screen.FromPoint(FormsCursor.Position).WorkingArea);
            drag.Card.IsInFlight = true;
            drag.Card.Press.Target = 0;
            drag.IsDragging = true;
            ApplyStripVisuals();
        }

        _transition.SetOffset(offsetX, offsetY);
    }

    private async void Card_MouseLeftButtonUp(object sender, MouseButtonEventArgs e)
    {
        if (_drag is not { } drag || !ReferenceEquals(drag.Card, sender))
        {
            return;
        }

        e.Handled = true;
        _drag = null;
        var card = drag.Card;
        card.Press.Target = 0;
        card.ReleaseMouseCapture();
        RequestFrame();

        if (!drag.IsDragging)
        {
            // Releasing outside the card cancels the click, like a button.
            var release = e.GetPosition(card);
            if (release.X >= 0 && release.Y >= 0 && release.X <= card.ActualWidth && release.Y <= card.ActualHeight)
            {
                await SwitchStageAsync([card.Key], card.Group.Front.Handle, activate: true);
            }

            return;
        }

        if (_transition is null)
        {
            return;
        }

        if (IsDropOnStage(FormsCursor.Position))
        {
            // Dropping a card onto the desktop adds its app to the current stage.
            var origins = _transition.GetCurrentPoses();
            await SwitchStageAsync([.. _stageKeys, card.Key], card.Group.Front.Handle, activate: true, origins);
            return;
        }

        await ReturnDraggedCardAsync(card);
    }

    private void Card_LostMouseCapture(object sender, MouseEventArgs e)
    {
        if (_drag is { } drag && ReferenceEquals(drag.Card, sender))
        {
            _drag = null;
            drag.Card.Press.Target = 0;
            RequestFrame();
            if (drag.IsDragging)
            {
                _ = ReturnDraggedCardAsync(drag.Card);
            }
        }
    }

    private async Task ReturnDraggedCardAsync(WindowPreviewControl card)
    {
        if (_transition is null)
        {
            return;
        }

        _isTransitioning = true;
        try
        {
            var cardShadow = GetCardShadow();
            var targets = card.GetLayers()
                .Select(layer => (layer.Handle, Rect: GetCardScreenRect(card, layer.Handle)))
                .Where(static target => target.Rect is not null)
                .ToDictionary(
                    static target => target.Handle,
                    target => new FlightPose(target.Rect!.Value, (float)card.Tilt, cardShadow));
            _transition.Retarget(targets);
            await _transition.AnimateAsync(ReturnDuration);
            card.IsInFlight = false;
            ApplyStripVisuals();
            await _transition.FadeOutShadowsAsync(LandingFrames);
        }
        finally
        {
            card.IsInFlight = false;
            ApplyStripVisuals();
            RequestFrame();
            _transition.End();
            _isTransitioning = false;
            RefreshWindows();
        }
    }

    private bool IsDropOnStage(System.Drawing.Point cursor) =>
        NativeMethods.GetWindowRect(_windowHandle, out var panel) && cursor.X > panel.Right + 24;

    private sealed class CardDrag(WindowPreviewControl card, System.Drawing.Point start)
    {
        internal WindowPreviewControl Card { get; } = card;

        internal System.Drawing.Point Start { get; } = start;

        internal bool IsDragging { get; set; }
    }
}
