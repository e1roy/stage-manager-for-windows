using System.Windows;
using System.Windows.Controls;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Effects;
using StageManager.Interop;
using WpfColor = System.Windows.Media.Color;
using WpfImage = System.Windows.Controls.Image;

namespace StageManager;

public partial class IconOverlayWindow : Window
{
    private readonly Dictionary<string, Border> _badges = new(StringComparer.Ordinal);

    internal IconOverlayWindow(Window owner)
    {
        InitializeComponent();
        Owner = owner;
        SourceInitialized += IconOverlayWindow_SourceInitialized;
    }

    internal void SyncToOwner(Window owner)
    {
        if (!double.IsFinite(owner.Left) ||
            !double.IsFinite(owner.Top) ||
            owner.ActualWidth <= 0 ||
            owner.ActualHeight <= 0)
        {
            return;
        }

        Left = owner.Left;
        Top = owner.Top;
        Width = owner.ActualWidth;
        Height = owner.ActualHeight;
        Opacity = owner.Opacity;
    }

    internal void UpdateBadges(IReadOnlyCollection<IconOverlayItem> items)
    {
        var visibleKeys = items.Select(static item => item.Key).ToHashSet(StringComparer.Ordinal);
        foreach (var (key, badge) in _badges)
        {
            if (!visibleKeys.Contains(key))
            {
                badge.Visibility = Visibility.Collapsed;
            }
        }

        foreach (var item in items)
        {
            if (!_badges.TryGetValue(item.Key, out var badge))
            {
                badge = CreateBadge(item.Icon);
                _badges.Add(item.Key, badge);
                IconCanvas.Children.Add(badge);
            }

            if (badge.Child is WpfImage image && !ReferenceEquals(image.Source, item.Icon))
            {
                image.Source = item.Icon;
            }

            badge.Width = item.Bounds.Width;
            badge.Height = item.Bounds.Height;
            badge.Opacity = item.Opacity;
            badge.Visibility = Visibility.Visible;
            Canvas.SetLeft(badge, item.Bounds.Left);
            Canvas.SetTop(badge, item.Bounds.Top);
        }
    }

    /// <summary>
    /// Puts the icons above the transition layer, which places itself on top
    /// when flights begin, so an icon shown before its windows land is not
    /// covered by them. The strip itself stays below the flights.
    /// </summary>
    internal void BringToTop()
    {
        var handle = new WindowInteropHelper(this).Handle;
        // Behind a full-screen app the icons stay out of the topmost band.
        if (handle == 0 || !Topmost)
        {
            return;
        }

        NativeMethods.SetWindowPos(
            handle,
            NativeMethods.HwndTopmost,
            0,
            0,
            0,
            0,
            NativeMethods.SwpNoMove |
            NativeMethods.SwpNoSize |
            NativeMethods.SwpNoActivate |
            NativeMethods.SwpNoOwnerZOrder);
    }

    internal void RemoveBadge(string key)
    {
        if (_badges.Remove(key, out var badge))
        {
            IconCanvas.Children.Remove(badge);
        }
    }

    internal void ClearBadges()
    {
        IconCanvas.Children.Clear();
        _badges.Clear();
    }

    private void IconOverlayWindow_SourceInitialized(object? sender, EventArgs e)
    {
        var handle = new WindowInteropHelper(this).Handle;
        var style = NativeMethods.GetWindowLongPtr(handle, NativeMethods.GwlExStyle).ToInt64();
        style |= NativeMethods.WsExToolWindow |
                 NativeMethods.WsExNoActivate |
                 NativeMethods.WsExTransparent;
        NativeMethods.SetWindowLongPtr(handle, NativeMethods.GwlExStyle, (nint)style);
        StageManager.Services.WindowCatalog.SetTransitionsDisabled(handle, true);
    }

    // Like macOS, the app icon stands on its own with a soft shadow, no plate.
    private static Border CreateBadge(ImageSource icon) => new()
    {
        IsHitTestVisible = false,
        Effect = new DropShadowEffect
        {
            BlurRadius = 12,
            ShadowDepth = 3,
            Direction = 270,
            Opacity = 0.45,
            Color = WpfColor.FromRgb(0, 0, 0)
        },
        Child = CreateIconImage(icon)
    };

    private static WpfImage CreateIconImage(ImageSource icon)
    {
        var image = new WpfImage
        {
            Source = icon,
            Stretch = Stretch.Uniform,
            IsHitTestVisible = false
        };
        RenderOptions.SetBitmapScalingMode(image, BitmapScalingMode.HighQuality);
        return image;
    }
}
