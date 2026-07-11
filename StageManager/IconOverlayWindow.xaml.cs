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
    private readonly Dictionary<nint, Border> _badges = [];

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
        var visibleHandles = items.Select(static item => item.Handle).ToHashSet();
        foreach (var (handle, badge) in _badges)
        {
            if (!visibleHandles.Contains(handle))
            {
                badge.Visibility = Visibility.Collapsed;
            }
        }

        foreach (var item in items)
        {
            if (!_badges.TryGetValue(item.Handle, out var badge))
            {
                badge = CreateBadge(item.Icon);
                _badges.Add(item.Handle, badge);
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
    }

    private static Border CreateBadge(ImageSource icon) => new()
    {
        Padding = new Thickness(2),
        Background = new SolidColorBrush(WpfColor.FromRgb(17, 22, 29)),
        BorderBrush = new SolidColorBrush(WpfColor.FromRgb(52, 65, 84)),
        BorderThickness = new Thickness(1),
        CornerRadius = new CornerRadius(8),
        IsHitTestVisible = false,
        Effect = new DropShadowEffect
        {
            BlurRadius = 9,
            ShadowDepth = 3,
            Opacity = 0.65,
            Color = WpfColor.FromArgb(208, 0, 0, 0)
        },
        Child = new WpfImage
        {
            Source = icon,
            Stretch = Stretch.Uniform,
            IsHitTestVisible = false
        }
    };
}
