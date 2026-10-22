using System.ComponentModel;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Windows;
using System.Windows.Forms;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Threading;
using StageManager.Interop;
using StageManager.Models;
using StageManager.Services;
using Application = System.Windows.Application;
using DrawingIcon = System.Drawing.Icon;
using FormsCursor = System.Windows.Forms.Cursor;
using MouseEventArgs = System.Windows.Input.MouseEventArgs;

namespace StageManager;

public partial class MainWindow : Window
{
    private const int HotKeyId = 0x534D;
#if DEBUG
    private static readonly int DebugSelectCardMessage = (int)RegisterWindowMessage("StageBar.Debug.SelectCard");

    [System.Runtime.InteropServices.DllImport("user32.dll", CharSet = System.Runtime.InteropServices.CharSet.Unicode)]
    private static extern uint RegisterWindowMessage(string message);
#endif
    private static readonly TimeSpan RefreshInterval = TimeSpan.FromMilliseconds(1500);
    private static readonly TimeSpan EdgePollInterval = TimeSpan.FromMilliseconds(16);
    private static readonly TimeSpan AutoHideDelay = TimeSpan.FromMilliseconds(650);

    private readonly WindowCatalog _windowCatalog = new();
    private readonly WindowIconService _windowIconService = new();
    private readonly SettingsService _settingsService = new();
    private readonly WindowParkingService _parking = new();
    private readonly DispatcherTimer _refreshTimer = new() { Interval = RefreshInterval };
    private readonly DispatcherTimer _edgeTimer = new() { Interval = EdgePollInterval };
    private readonly Spring _panelReveal = new(0, response: 0.32);
    private readonly Stopwatch _frameClock = Stopwatch.StartNew();
    private readonly DrawingIcon _applicationIcon;
    private readonly NotifyIcon _trayIcon;
    private StageManagerSettings _settings;
    private SettingsWindow? _settingsWindow;
    private IconOverlayWindow? _iconOverlay;
    private StageTransitionWindow? _transition;
    private WinEventWatcher? _winEvents;
    private AppBarService? _appBar;
    private HwndSource? _source;
    private nint _windowHandle;
    private DateTime _lastInsideAt = DateTime.MinValue;
    private bool _isExiting;
    private bool _isRenderingSubscribed;
    private double _lastFrameTime;
    private double _shownLeft;
    private Screen? _currentScreen;
    private DateTime _lastAppBarNotification = DateTime.MinValue;

    public MainWindow()
    {
        InitializeComponent();

        _settings = _settingsService.Load();
        _applicationIcon = LoadApplicationIcon();
        _trayIcon = CreateTrayIcon();
        _refreshTimer.Tick += (_, _) => RefreshWindows();
        _edgeTimer.Tick += (_, _) => PollScreenEdge();
        _eventRefreshTimer.Tick += (_, _) => RefreshWindows();
        _coverTimer.Tick += (_, _) => UpdatePanelCovered();
        _foregroundTimer.Tick += ForegroundTimer_Tick;
        MouseEnter += Panel_MouseEnter;
        MouseLeave += Panel_MouseLeave;
        SizeChanged += (_, _) => LayoutStrip();
        SourceInitialized += MainWindow_SourceInitialized;
        Loaded += MainWindow_Loaded;
    }

    private bool IsPanelShown => _panelReveal.Target > 0.5;

    private void MainWindow_SourceInitialized(object? sender, EventArgs e)
    {
        _windowHandle = new WindowInteropHelper(this).Handle;
        _source = HwndSource.FromHwnd(_windowHandle);
        _source?.AddHook(WindowProcedure);
        _appBar = new AppBarService(_windowHandle);

        var style = NativeMethods.GetWindowLongPtr(_windowHandle, NativeMethods.GwlExStyle).ToInt64();
        style |= NativeMethods.WsExToolWindow | NativeMethods.WsExNoActivate;
        NativeMethods.SetWindowLongPtr(_windowHandle, NativeMethods.GwlExStyle, (nint)style);
        // The strip animates its own reveal; a DWM show/hide fade on top of it reads as a flash.
        WindowCatalog.SetTransitionsDisabled(_windowHandle, true);
        NativeMethods.RegisterHotKey(
            _windowHandle,
            HotKeyId,
            NativeMethods.ModControl | NativeMethods.ModAlt,
            NativeMethods.VkSpace);
    }

    private void MainWindow_Loaded(object sender, RoutedEventArgs e)
    {
        _iconOverlay = new IconOverlayWindow(this);
        _transition = new StageTransitionWindow();
        _parking.RecoverFromJournal();
        _currentScreen = Screen.FromPoint(FormsCursor.Position);
        ApplySettingsLayout(initial: true);
        _winEvents = new WinEventWatcher();
        _winEvents.ForegroundChanged += WinEvents_ForegroundChanged;
        _winEvents.WindowChanged += WinEvents_WindowChanged;
        _refreshTimer.Start();
        _edgeTimer.Start();
        InitializeStage();
    }

    private void PollScreenEdge()
    {
        if (_settings.DisplayMode is DisplayMode.Fixed || _isExiting)
        {
            return;
        }

        var cursor = FormsCursor.Position;
        var screen = Screen.FromPoint(cursor);
        var isAtLeftEdge = cursor.X <= screen.Bounds.Left + 2 &&
                           cursor.Y >= screen.Bounds.Top &&
                           cursor.Y <= screen.Bounds.Bottom;

        if (isAtLeftEdge)
        {
            if (_currentScreen?.DeviceName != screen.DeviceName)
            {
                _currentScreen = screen;
                ApplySettingsLayout(initial: false);
            }

            _lastInsideAt = DateTime.UtcNow;
        }
        else if (IsPanelShown && IsCursorInsideWindow(cursor))
        {
            _lastInsideAt = DateTime.UtcNow;
        }

        // Like macOS, the strip stays out while it has room and steps aside
        // when a window on the stage covers it; the left edge brings it back.
        var shouldShow = DateTime.UtcNow - _lastInsideAt < AutoHideDelay ||
                         _isHoldingPanelOpen ||
                         _drag is not null ||
                         (!_isPanelCovered && _cards.Count > 0);
        if (shouldShow)
        {
            ShowPanel();
        }
        else
        {
            HidePanel();
        }
    }

    private bool IsCursorInsideWindow(System.Drawing.Point cursor)
    {
        if (_windowHandle != 0 && NativeMethods.GetWindowRect(_windowHandle, out var bounds))
        {
            return cursor.X >= bounds.Left && cursor.X <= bounds.Right &&
                   cursor.Y >= bounds.Top && cursor.Y <= bounds.Bottom;
        }

        return false;
    }

    private void ApplySettingsLayout(bool initial)
    {
        _settings = SettingsService.Normalize(_settings);
        var screen = _currentScreen ?? Screen.FromPoint(FormsCursor.Position);
        _currentScreen = screen;
        var source = PresentationSource.FromVisual(this);
        var scaleX = source?.CompositionTarget?.TransformToDevice.M11 ?? 1.0;
        var scaleY = source?.CompositionTarget?.TransformToDevice.M22 ?? 1.0;
        var layout = LayoutMetrics.Calculate(_settings.DisplayWidth, _settings.IconScale);
        if (layout != _cardLayout)
        {
            // Cards keep the geometry they were built with; rebuild them.
            ClearCards();
        }

        _cardLayout = layout;
        _itemExtent = _cardLayout.ItemExtent;

        if (_settings.DisplayMode is DisplayMode.Fixed && _appBar is not null)
        {
            _lastAppBarNotification = DateTime.UtcNow;
            var reserved = _appBar.Reserve(
                screen.Bounds,
                Math.Max(1, (int)Math.Round(_settings.DisplayWidth * scaleX)));
            Width = reserved.Width / scaleX;
            Height = reserved.Height / scaleY;
            Left = reserved.Left / scaleX;
            Top = reserved.Top / scaleY;
            _shownLeft = Left;
        }
        else
        {
            // The strip spans the work area height like Fixed mode, so cards
            // can use all of it; only the band they occupy reacts to windows.
            _appBar?.Unregister();
            var workArea = screen.WorkingArea;
            Width = _settings.DisplayWidth;
            Height = workArea.Height / scaleY;
            _shownLeft = (screen.Bounds.Left + 4) / scaleX;
            Left = _shownLeft;
            Top = workArea.Top / scaleY;
        }

        RefreshWindows();
        if (_settings.DisplayMode is DisplayMode.Fixed)
        {
            ShowPanel();
            _panelReveal.Snap(1);
            ApplyStripVisuals();
        }
        else
        {
            HidePanel(immediate: true);
            if (!initial)
            {
                _lastInsideAt = DateTime.UtcNow;
            }
        }
    }

    private void ShowPanel()
    {
        if (IsPanelShown)
        {
            return;
        }

        _panelReveal.Target = 1;
        Visibility = Visibility.Visible;
        if (_windowHandle != 0)
        {
            NativeMethods.ShowWindowAsync(_windowHandle, NativeMethods.SwShowNoActivate);
        }

        if (_iconOverlay is not null)
        {
            _iconOverlay.SyncToOwner(this);
            if (!_iconOverlay.IsVisible)
            {
                _iconOverlay.Show();
            }
        }

        RequestFrame();
    }

    private void HidePanel(bool immediate = false)
    {
        if (_settings.DisplayMode is DisplayMode.Fixed && !immediate)
        {
            return;
        }

        _panelReveal.Target = 0;
        if (immediate)
        {
            _panelReveal.Snap(0);
            ApplyStripVisuals();
            ConcealWindow();
            return;
        }

        RequestFrame();
    }

    /// <summary>Hides the HWND once the strip has slid away so it stops intercepting clicks.</summary>
    private void ConcealWindow()
    {
        _iconOverlay?.Hide();
        Visibility = Visibility.Hidden;
    }

    private void TogglePanel()
    {
        if (_settings.DisplayMode is DisplayMode.Fixed)
        {
            ShowPanel();
            return;
        }

        if (IsPanelShown)
        {
            _lastInsideAt = DateTime.MinValue;
            HidePanel();
        }
        else
        {
            var screen = Screen.FromPoint(FormsCursor.Position);
            if (_currentScreen?.DeviceName != screen.DeviceName)
            {
                _currentScreen = screen;
                ApplySettingsLayout(initial: false);
            }

            _lastInsideAt = DateTime.UtcNow;
            ShowPanel();
        }
    }

    private void Panel_MouseEnter(object sender, MouseEventArgs e) => _lastInsideAt = DateTime.UtcNow;

    private void Panel_MouseLeave(object sender, MouseEventArgs e) => _lastInsideAt = DateTime.UtcNow;

    private nint WindowProcedure(nint window, int message, nint wParam, nint lParam, ref bool handled)
    {
        if (message == NativeMethods.WmHotKey && wParam.ToInt32() == HotKeyId)
        {
            TogglePanel();
            handled = true;
        }
#if DEBUG
        else if (message == DebugSelectCardMessage)
        {
            // Development aid: lets an automated test select strip card N
            // through the same path as a click, without moving the mouse.
            if (wParam.ToInt32() is var index && index >= 0 && index < _cards.Count)
            {
                var card = _cards[index];
                _ = SwitchStageAsync([card.Key], card.Group.Front.Handle, activate: true);
            }

            handled = true;
        }
#endif
        else if (_appBar is not null &&
                 message == _appBar.CallbackMessage &&
                 _settings.DisplayMode is DisplayMode.Fixed)
        {
            switch (wParam.ToInt32())
            {
                // Only a real change of the taskbar or other app bars needs a
                // new reservation; re-reserving on every notice makes the shell
                // rearrange the taskbar again.
                case AppBarService.PositionChangedNotification
                    when DateTime.UtcNow - _lastAppBarNotification > TimeSpan.FromMilliseconds(500):
                    _lastAppBarNotification = DateTime.UtcNow;
                    Dispatcher.BeginInvoke(() => ApplySettingsLayout(initial: false), DispatcherPriority.Background);
                    break;
                // App bars step behind full-screen apps such as videos and games.
                case AppBarService.FullScreenAppNotification:
                    var isFullScreen = lParam != 0;
                    Topmost = !isFullScreen;
                    if (_iconOverlay is not null)
                    {
                        _iconOverlay.Topmost = !isFullScreen;
                    }

                    break;
            }

            handled = true;
        }

        return 0;
    }

    private NotifyIcon CreateTrayIcon()
    {
        var menu = new ContextMenuStrip();
        menu.Items.Add("显示 / 隐藏", null, (_, _) => Dispatcher.Invoke(TogglePanel));
        menu.Items.Add("设置…", null, (_, _) => Dispatcher.Invoke(OpenSettings));
        menu.Items.Add(new ToolStripSeparator());
        menu.Items.Add("退出", null, (_, _) => Dispatcher.Invoke(ExitApplication));

        var icon = new NotifyIcon
        {
            Icon = _applicationIcon,
            Text = "StageBar",
            Visible = true,
            ContextMenuStrip = menu
        };
        icon.DoubleClick += (_, _) => Dispatcher.Invoke(TogglePanel);
        return icon;
    }

    private static DrawingIcon LoadApplicationIcon()
    {
        try
        {
            if (Environment.ProcessPath is { } executablePath &&
                DrawingIcon.ExtractAssociatedIcon(executablePath) is { } icon)
            {
                return icon;
            }
        }
        catch (ArgumentException)
        {
            // Fall back to the system icon when the executable icon cannot be read.
        }

        return (DrawingIcon)SystemIcons.Application.Clone();
    }

    internal void OpenSettings()
    {
        if (_settingsWindow is not null)
        {
            _settingsWindow.Activate();
            return;
        }

        var window = new SettingsWindow(_settings);
        _settingsWindow = window;
        window.SettingsChanged += ApplySettingsFromWindow;
        window.Closed += (_, _) => _settingsWindow = null;
        window.Show();
        window.Activate();
    }

    private void ApplySettingsFromWindow(StageManagerSettings settings)
    {
        var normalized = SettingsService.Normalize(settings);
        if (normalized == _settings)
        {
            return;
        }

        var hidingChanged = normalized.HideInactiveWindows != _settings.HideInactiveWindows;
        _settings = normalized;
        try
        {
            _settingsService.Save(_settings);
        }
        catch (IOException)
        {
            // The applied in-memory settings remain valid even if persistence fails.
        }

        if (hidingChanged)
        {
            if (_settings.HideInactiveWindows)
            {
                ParkInactiveWindows(BuildGroups(_windowCatalog.GetWindows()));
            }
            else
            {
                _parking.RestoreAll();
            }
        }

        ApplySettingsLayout(initial: false);
    }

    /// <summary>Puts every parked window back where it was. Safe to call while shutting down.</summary>
    internal void RestoreParkedWindows() => _parking.RestoreAll();

    private void ExitApplication()
    {
        _isExiting = true;
        Close();
    }

    protected override void OnClosing(CancelEventArgs e)
    {
        if (!_isExiting)
        {
            e.Cancel = true;
            HidePanel();
            return;
        }

        _refreshTimer.Stop();
        _edgeTimer.Stop();
        _eventRefreshTimer.Stop();
        _coverTimer.Stop();
        _foregroundTimer.Stop();
        _winEvents?.Dispose();
        UnsubscribeRendering();
        _transition?.Dispose();
        _transition = null;
        _parking.RestoreAll();
        ClearCards();
        _iconOverlay?.ClearBadges();
        _iconOverlay?.Close();
        _iconOverlay = null;
        _appBar?.Dispose();
        if (_windowHandle != 0)
        {
            NativeMethods.UnregisterHotKey(_windowHandle, HotKeyId);
        }

        _source?.RemoveHook(WindowProcedure);
        _trayIcon.Visible = false;
        _trayIcon.Dispose();
        _applicationIcon.Dispose();
        base.OnClosing(e);
        Application.Current.Shutdown();
    }
}
