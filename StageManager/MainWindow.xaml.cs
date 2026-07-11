using System.ComponentModel;
using System.Drawing;
using System.IO;
using System.Windows;
using System.Windows.Forms;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Animation;
using System.Windows.Threading;
using StageManager.Controls;
using StageManager.Interop;
using StageManager.Models;
using StageManager.Services;
using Application = System.Windows.Application;
using DrawingIcon = System.Drawing.Icon;
using FormsCursor = System.Windows.Forms.Cursor;
using MouseEventArgs = System.Windows.Input.MouseEventArgs;
using MouseWheelEventArgs = System.Windows.Input.MouseWheelEventArgs;
using Point = System.Windows.Point;

namespace StageManager;

public partial class MainWindow : Window
{
    private const int HotKeyId = 0x534D;
    private static readonly TimeSpan RefreshInterval = TimeSpan.FromMilliseconds(1400);
    private static readonly TimeSpan EdgePollInterval = TimeSpan.FromMilliseconds(90);

    private readonly WindowCatalog _windowCatalog = new();
    private readonly WindowIconService _windowIconService = new();
    private readonly SettingsService _settingsService = new();
    private readonly Dictionary<nint, DwmThumbnail> _thumbnails = [];
    private readonly DispatcherTimer _refreshTimer = new() { Interval = RefreshInterval };
    private readonly DispatcherTimer _edgeTimer = new() { Interval = EdgePollInterval };
    private readonly ScrollOffsetAnimator _scrollAnimator;
    private readonly DrawingIcon _applicationIcon;
    private readonly NotifyIcon _trayIcon;
    private StageManagerSettings _settings;
    private SettingsWindow? _settingsWindow;
    private IconOverlayWindow? _iconOverlay;
    private AppBarService? _appBar;
    private HwndSource? _source;
    private nint _windowHandle;
    private DateTime _lastInsideAt = DateTime.UtcNow;
    private bool _isPanelVisible;
    private bool _isExiting;
    private bool _isLaunching;
    private bool _isRenderingSubscribed;
    private int _carouselStartIndex;
    private int _visibleCardCount = 3;
    private double _itemExtent = 158;
    private double _shownLeft;
    private Screen? _currentScreen;
    private DateTime _lastAppBarNotification = DateTime.MinValue;

    public MainWindow()
    {
        InitializeComponent();

        _settings = _settingsService.Load();
        _scrollAnimator = new ScrollOffsetAnimator(PreviewScrollViewer);
        _applicationIcon = LoadApplicationIcon();
        _trayIcon = CreateTrayIcon();
        _refreshTimer.Tick += (_, _) => RefreshWindowList();
        _edgeTimer.Tick += (_, _) => PollScreenEdge();
        MouseEnter += Panel_MouseEnter;
        MouseLeave += Panel_MouseLeave;
        LocationChanged += (_, _) => Dispatcher.BeginInvoke(UpdateVisualLayers, DispatcherPriority.Render);
        SizeChanged += (_, _) => Dispatcher.BeginInvoke(UpdateVisualLayers, DispatcherPriority.Render);
        SourceInitialized += MainWindow_SourceInitialized;
        Loaded += MainWindow_Loaded;
    }

    private void MainWindow_SourceInitialized(object? sender, EventArgs e)
    {
        _windowHandle = new WindowInteropHelper(this).Handle;
        _source = HwndSource.FromHwnd(_windowHandle);
        _source?.AddHook(WindowProcedure);
        _appBar = new AppBarService(_windowHandle);

        var style = NativeMethods.GetWindowLongPtr(_windowHandle, NativeMethods.GwlExStyle).ToInt64();
        style |= NativeMethods.WsExToolWindow | NativeMethods.WsExNoActivate;
        NativeMethods.SetWindowLongPtr(_windowHandle, NativeMethods.GwlExStyle, (nint)style);
        NativeMethods.RegisterHotKey(
            _windowHandle,
            HotKeyId,
            NativeMethods.ModControl | NativeMethods.ModAlt,
            NativeMethods.VkSpace);
    }

    private void MainWindow_Loaded(object sender, RoutedEventArgs e)
    {
        _iconOverlay = new IconOverlayWindow(this);
        _currentScreen = Screen.FromPoint(FormsCursor.Position);
        ApplySettingsLayout(initial: true);
        _refreshTimer.Start();
        _edgeTimer.Start();
    }

    private void RefreshWindowList(bool force = false)
    {
        var windows = _windowCatalog.GetWindows();
        var handles = windows.Select(static window => window.Handle).ToHashSet();
        var currentHandles = PreviewList.Children
            .OfType<WindowPreviewControl>()
            .Select(static control => control.Snapshot.Handle)
            .ToHashSet();

        if (!force && handles.SetEquals(currentHandles))
        {
            UpdateVisualLayers();
            return;
        }

        DisposeThumbnails();
        _iconOverlay?.ClearBadges();
        PreviewList.Children.Clear();
        var layout = LayoutMetrics.Calculate(_settings.DisplayWidth);
        _itemExtent = layout.ItemExtent;
        foreach (var window in windows)
        {
            var control = new WindowPreviewControl(
                window,
                _windowIconService.GetIcon(window.Handle),
                layout);
            control.WindowSelected += Preview_WindowSelected;
            PreviewList.Children.Add(control);
        }

        _carouselStartIndex = Math.Clamp(_carouselStartIndex, 0, Math.Max(0, windows.Count - _visibleCardCount));
        PreviewScrollViewer.ScrollToVerticalOffset(_carouselStartIndex * _itemExtent);
        UpdateCarouselVisuals();
        EmptyState.Visibility = windows.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
        Dispatcher.BeginInvoke(RegisterThumbnails, DispatcherPriority.Loaded);
    }

    private void RegisterThumbnails()
    {
        if (_windowHandle == 0)
        {
            return;
        }

        foreach (var control in PreviewList.Children.OfType<WindowPreviewControl>())
        {
            if (_thumbnails.ContainsKey(control.Snapshot.Handle))
            {
                continue;
            }

            if (DwmThumbnail.TryCreate(_windowHandle, control.Snapshot.Handle) is { } thumbnail)
            {
                _thumbnails.Add(control.Snapshot.Handle, thumbnail);
            }
        }

        UpdateVisualLayers();
    }

    private void UpdateThumbnailBounds()
    {
        if (!_isPanelVisible ||
            PresentationSource.FromVisual(this)?.CompositionTarget is not { } compositionTarget ||
            PreviewScrollViewer.ViewportWidth <= 0 ||
            PreviewScrollViewer.ViewportHeight <= 0)
        {
            return;
        }

        var toDevice = compositionTarget.TransformToDevice;
        var viewportOrigin = PreviewScrollViewer.TransformToAncestor(this).Transform(new Point(0, 0));
        var viewportBottomRight = PreviewScrollViewer.TransformToAncestor(this).Transform(new Point(
            PreviewScrollViewer.ViewportWidth,
            PreviewScrollViewer.ViewportHeight));
        var deviceViewportOrigin = toDevice.Transform(viewportOrigin);
        var deviceViewportBottomRight = toDevice.Transform(viewportBottomRight);
        var clipBounds = new NativeMethods.Rect
        {
            Left = (int)Math.Floor(deviceViewportOrigin.X),
            Top = (int)Math.Floor(deviceViewportOrigin.Y),
            Right = (int)Math.Ceiling(deviceViewportBottomRight.X),
            Bottom = (int)Math.Ceiling(deviceViewportBottomRight.Y)
        };

        foreach (var control in PreviewList.Children.OfType<WindowPreviewControl>())
        {
            if (!_thumbnails.TryGetValue(control.Snapshot.Handle, out var thumbnail))
            {
                continue;
            }

            if (!control.ThumbnailSurface.IsVisible ||
                control.ThumbnailSurface.ActualWidth <= 0 ||
                control.ThumbnailSurface.ActualHeight <= 0)
            {
                thumbnail.SetVisible(false);
                continue;
            }

            try
            {
                var transform = control.ThumbnailSurface.TransformToAncestor(this);
                var origin = transform.Transform(new Point(0, 0));
                var bottomRight = transform.Transform(new Point(
                    control.ThumbnailSurface.ActualWidth,
                    control.ThumbnailSurface.ActualHeight));
                var deviceOrigin = toDevice.Transform(origin);
                var deviceBottomRight = toDevice.Transform(bottomRight);

                thumbnail.Update(
                    new NativeMethods.Rect
                    {
                        Left = (int)Math.Round(deviceOrigin.X),
                        Top = (int)Math.Round(deviceOrigin.Y),
                        Right = (int)Math.Round(deviceBottomRight.X),
                        Bottom = (int)Math.Round(deviceBottomRight.Y)
                    },
                    clipBounds,
                    (byte)Math.Clamp((int)Math.Round(control.Opacity * 255), 0, 255));
            }
            catch (InvalidOperationException)
            {
                thumbnail.SetVisible(false);
            }
        }
    }

    private void UpdateIconOverlay()
    {
        if (_iconOverlay is null || !_isPanelVisible || !_iconOverlay.IsVisible)
        {
            return;
        }

        _iconOverlay.SyncToOwner(this);
        if (PreviewScrollViewer.ViewportWidth <= 0 || PreviewScrollViewer.ViewportHeight <= 0)
        {
            _iconOverlay.UpdateBadges([]);
            return;
        }

        var viewportTransform = PreviewScrollViewer.TransformToAncestor(this);
        var viewportOrigin = viewportTransform.Transform(new Point(0, 0));
        var viewportBottomRight = viewportTransform.Transform(new Point(
            PreviewScrollViewer.ViewportWidth,
            PreviewScrollViewer.ViewportHeight));
        var viewportBounds = new Rect(viewportOrigin, viewportBottomRight);
        var items = new List<IconOverlayItem>();

        foreach (var control in PreviewList.Children.OfType<WindowPreviewControl>())
        {
            if (control.AppIcon is null ||
                control.Opacity <= 0.01 ||
                !control.IconAnchor.IsVisible ||
                control.IconAnchor.ActualWidth <= 0 ||
                control.IconAnchor.ActualHeight <= 0)
            {
                continue;
            }

            try
            {
                var transform = control.IconAnchor.TransformToAncestor(this);
                var origin = transform.Transform(new Point(0, 0));
                var bottomRight = transform.Transform(new Point(
                    control.IconAnchor.ActualWidth,
                    control.IconAnchor.ActualHeight));
                var bounds = new Rect(origin, bottomRight);
                if (bounds.IntersectsWith(viewportBounds))
                {
                    items.Add(new IconOverlayItem(
                        control.Snapshot.Handle,
                        control.AppIcon,
                        bounds,
                        control.Opacity));
                }
            }
            catch (InvalidOperationException)
            {
                // A layout pass can briefly detach a card while the list refreshes.
            }
        }

        _iconOverlay.UpdateBadges(items);
    }

    private void UpdateVisualLayers()
    {
        UpdateThumbnailBounds();
        UpdateIconOverlay();
    }

    private void PreviewScrollViewer_ScrollChanged(object sender, System.Windows.Controls.ScrollChangedEventArgs e)
    {
        UpdateCarouselVisuals();
        Dispatcher.BeginInvoke(UpdateVisualLayers, DispatcherPriority.Render);
    }

    private void PreviewScrollViewer_PreviewMouseWheel(object sender, MouseWheelEventArgs e)
    {
        e.Handled = true;
        if (_isLaunching || PreviewList.Children.Count <= _visibleCardCount)
        {
            return;
        }

        var direction = e.Delta < 0 ? 1 : -1;
        var maxStart = Math.Max(0, PreviewList.Children.Count - _visibleCardCount);
        var nextIndex = Math.Clamp(_carouselStartIndex + direction, 0, maxStart);
        if (nextIndex == _carouselStartIndex)
        {
            return;
        }

        _carouselStartIndex = nextIndex;
        _scrollAnimator.AnimateTo(_carouselStartIndex * _itemExtent);
    }

    private void UpdateCarouselVisuals()
    {
        var count = PreviewList.Children.Count;
        var visibleCount = Math.Min(_visibleCardCount, Math.Max(1, count));
        var centerIndex = count <= 1
            ? 0d
            : PreviewScrollViewer.VerticalOffset / _itemExtent + (visibleCount - 1) / 2d;
        var maximumVisibleDistance = Math.Max(0, (visibleCount - 1) / 2d);

        var index = 0;
        foreach (var control in PreviewList.Children.OfType<WindowPreviewControl>())
        {
            control.SetCarouselPosition(index - centerIndex, maximumVisibleDistance);
            index++;
        }
    }

    private async void Preview_WindowSelected(object? sender, WindowSnapshot window)
    {
        if (_isLaunching || sender is not WindowPreviewControl control)
        {
            return;
        }

        _isLaunching = true;
        try
        {
            await control.PlayLaunchAnimationAsync();
            WindowCatalog.Activate(window.Handle);
            if (_settings.DisplayMode is DisplayMode.Floating)
            {
                HidePanel();
            }

            await Task.Delay(70);
        }
        finally
        {
            control.ResetAnimations();
            UpdateCarouselVisuals();
            _isLaunching = false;
        }
    }

    private void PollScreenEdge()
    {
        if (_settings.DisplayMode is DisplayMode.Fixed)
        {
            return;
        }

        var cursor = FormsCursor.Position;
        var screen = Screen.FromPoint(cursor);
        var isAtLeftEdge = cursor.X <= screen.Bounds.Left + 2 &&
                           cursor.Y >= screen.Bounds.Top &&
                           cursor.Y <= screen.Bounds.Bottom;
        var isInsidePanel = _isPanelVisible && IsCursorInsideWindow(cursor);

        if (isAtLeftEdge)
        {
            if (_currentScreen?.DeviceName != screen.DeviceName)
            {
                _currentScreen = screen;
                ApplySettingsLayout(initial: false);
            }
            ShowPanel();
            _lastInsideAt = DateTime.UtcNow;
            return;
        }

        if (isInsidePanel)
        {
            _lastInsideAt = DateTime.UtcNow;
        }
        else if (_isPanelVisible && DateTime.UtcNow - _lastInsideAt > TimeSpan.FromMilliseconds(650))
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
        var availableHeightDips = screen.WorkingArea.Height / scaleY;
        var maximumCards = LayoutMetrics.CalculateMaximumCardCount(
            _settings.DisplayWidth,
            availableHeightDips);
        _visibleCardCount = Math.Min(_settings.CardCount, maximumCards);
        var layout = LayoutMetrics.Calculate(_settings.DisplayWidth);
        _itemExtent = layout.ItemExtent;
        PreviewScrollViewer.Width = Math.Max(180, _settings.DisplayWidth - 10);
        PreviewScrollViewer.Height = _visibleCardCount * _itemExtent;

        BeginAnimation(LeftProperty, null);
        BeginAnimation(OpacityProperty, null);
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
            _appBar?.Unregister();
            var workArea = screen.WorkingArea;
            Width = _settings.DisplayWidth;
            Height = Math.Min(
                availableHeightDips,
                _visibleCardCount * _itemExtent + 24);
            _shownLeft = (screen.Bounds.Left + 4) / scaleX;
            Left = _shownLeft;
            Top = (workArea.Top + (workArea.Height - Height * scaleY) / 2.0) / scaleY;
        }

        RefreshWindowList(force: true);
        if (_settings.DisplayMode is DisplayMode.Fixed)
        {
            _isPanelVisible = false;
            ShowPanel();
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
        if (_isPanelVisible)
        {
            return;
        }

        _isPanelVisible = true;
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

        BeginAnimation(OpacityProperty, null);
        Opacity = 1;
        if (_settings.DisplayMode is DisplayMode.Floating)
        {
            BeginAnimation(LeftProperty, null);
            Left = _shownLeft;
            var hiddenLeft = _shownLeft - Width + 12;
            BeginAnimation(LeftProperty, new DoubleAnimation(hiddenLeft, _shownLeft, TimeSpan.FromMilliseconds(230))
            {
                EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut }
            });
            BeginAnimation(OpacityProperty, new DoubleAnimation(0, 1, TimeSpan.FromMilliseconds(170)));
        }
        SubscribeRendering();
        Dispatcher.BeginInvoke(UpdateVisualLayers, DispatcherPriority.Render);
    }

    private void HidePanel(bool immediate = false)
    {
        if (_settings.DisplayMode is DisplayMode.Fixed && !immediate)
        {
            return;
        }

        if (!_isPanelVisible && !immediate)
        {
            return;
        }

        _isPanelVisible = false;
        if (immediate)
        {
            UnsubscribeRendering();
            _iconOverlay?.Hide();
            Opacity = 0;
            Visibility = Visibility.Hidden;
            BeginAnimation(LeftProperty, null);
            Left = _shownLeft;
            return;
        }

        var animation = new DoubleAnimation(Opacity, 0, TimeSpan.FromMilliseconds(140));
        var currentLeft = Left;
        var hiddenLeft = _shownLeft - Width + 12;
        BeginAnimation(LeftProperty, new DoubleAnimation(currentLeft, hiddenLeft, TimeSpan.FromMilliseconds(210))
        {
            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseIn }
        });
        animation.Completed += (_, _) =>
        {
            if (!_isPanelVisible)
            {
                UnsubscribeRendering();
                _iconOverlay?.Hide();
                Visibility = Visibility.Hidden;
                BeginAnimation(LeftProperty, null);
                Left = _shownLeft;
            }
        };
        BeginAnimation(OpacityProperty, animation);
    }

    private void TogglePanel()
    {
        if (_settings.DisplayMode is DisplayMode.Fixed)
        {
            ShowPanel();
            return;
        }

        if (_isPanelVisible)
        {
            HidePanel();
        }
        else
        {
            _currentScreen = Screen.FromPoint(FormsCursor.Position);
            ApplyFloatingPosition();
            ShowPanel();
        }
    }

    private void ApplyFloatingPosition()
    {
        if (_settings.DisplayMode is not DisplayMode.Floating || _currentScreen is null)
        {
            return;
        }

        var source = PresentationSource.FromVisual(this);
        var scaleX = source?.CompositionTarget?.TransformToDevice.M11 ?? 1.0;
        var scaleY = source?.CompositionTarget?.TransformToDevice.M22 ?? 1.0;
        var workArea = _currentScreen.WorkingArea;
        BeginAnimation(LeftProperty, null);
        _shownLeft = (_currentScreen.Bounds.Left + 4) / scaleX;
        Left = _shownLeft;
        Top = (workArea.Top + (workArea.Height - Height * scaleY) / 2.0) / scaleY;
    }

    private void SubscribeRendering()
    {
        if (_isRenderingSubscribed)
        {
            return;
        }

        CompositionTarget.Rendering += CompositionTarget_Rendering;
        _isRenderingSubscribed = true;
    }

    private void UnsubscribeRendering()
    {
        if (!_isRenderingSubscribed)
        {
            return;
        }

        CompositionTarget.Rendering -= CompositionTarget_Rendering;
        _isRenderingSubscribed = false;
    }

    private void CompositionTarget_Rendering(object? sender, EventArgs e) => UpdateVisualLayers();

    private void Panel_MouseEnter(object sender, MouseEventArgs e) => _lastInsideAt = DateTime.UtcNow;

    private void Panel_MouseLeave(object sender, MouseEventArgs e) => _lastInsideAt = DateTime.UtcNow;

    private nint WindowProcedure(nint window, int message, nint wParam, nint lParam, ref bool handled)
    {
        if (message == NativeMethods.WmHotKey && wParam.ToInt32() == HotKeyId)
        {
            TogglePanel();
            handled = true;
        }
        else if (_appBar is not null &&
                 message == _appBar.CallbackMessage &&
                 _settings.DisplayMode is DisplayMode.Fixed &&
                 DateTime.UtcNow - _lastAppBarNotification > TimeSpan.FromMilliseconds(500))
        {
            _lastAppBarNotification = DateTime.UtcNow;
            Dispatcher.BeginInvoke(() => ApplySettingsLayout(initial: false), DispatcherPriority.Background);
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

        _settings = normalized;
        try
        {
            _settingsService.Save(_settings);
        }
        catch (IOException)
        {
            // The applied in-memory settings remain valid even if persistence fails.
        }

        ApplySettingsLayout(initial: false);
    }

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
        UnsubscribeRendering();
        DisposeThumbnails();
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

    private void DisposeThumbnails()
    {
        foreach (var thumbnail in _thumbnails.Values)
        {
            thumbnail.Dispose();
        }

        _thumbnails.Clear();
    }
}
