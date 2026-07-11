using System.Windows;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Animation;
using StageManager.Models;
using StageManager.Services;

namespace StageManager.Controls;

public partial class WindowPreviewControl : System.Windows.Controls.UserControl
{
    internal WindowPreviewControl(WindowSnapshot snapshot, ImageSource? icon, CardLayout layout)
    {
        InitializeComponent();
        Snapshot = snapshot;
        ItemExtent = layout.ItemExtent;
        Width = layout.PreviewWidth + 20;
        Height = layout.ItemExtent;
        PreviewVisual.Width = layout.PreviewWidth;
        PreviewVisual.Height = layout.PreviewHeight;
        PreviewSurface.Width = layout.PreviewWidth;
        PreviewSurface.Height = layout.PreviewHeight;
        DepthOverlay.Width = layout.PreviewWidth;
        DepthOverlay.Height = layout.PreviewHeight;
        AppIcon = icon;
        AppIconAnchor.Visibility = icon is null ? Visibility.Collapsed : Visibility.Visible;
    }

    public WindowSnapshot Snapshot { get; }

    public double ItemExtent { get; }

    public ImageSource? AppIcon { get; }

    public FrameworkElement IconAnchor => AppIconAnchor;

    public FrameworkElement ThumbnailSurface => PreviewSurface;

    public void SetCarouselPosition(double distanceFromCenter, double maximumVisibleDistance)
    {
        var absoluteDistance = Math.Abs(distanceFromCenter);
        var isVisible = absoluteDistance <= maximumVisibleDistance + 0.58;

        CardScale.ScaleX = 1;
        CardScale.ScaleY = 1;
        Opacity = isVisible ? 1 : 0;
        IsHitTestVisible = isVisible;
        System.Windows.Controls.Panel.SetZIndex(this, 100 - (int)Math.Round(absoluteDistance * 10));
    }

    public Task PlayLaunchAnimationAsync()
    {
        IsHitTestVisible = false;
        var completion = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var duration = TimeSpan.FromMilliseconds(390);

        var slide = new DoubleAnimationUsingKeyFrames
        {
            Duration = duration,
            FillBehavior = FillBehavior.HoldEnd
        };
        slide.KeyFrames.Add(new EasingDoubleKeyFrame(0, KeyTime.FromPercent(0)));
        slide.KeyFrames.Add(new EasingDoubleKeyFrame(-10, KeyTime.FromPercent(0.16))
        {
            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut }
        });
        slide.KeyFrames.Add(new EasingDoubleKeyFrame(430, KeyTime.FromPercent(1))
        {
            EasingFunction = new ExponentialEase { EasingMode = EasingMode.EaseIn, Exponent = 3.2 }
        });

        var scale = new DoubleAnimation(CardScale.ScaleX, 1.1, duration)
        {
            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut }
        };
        var fade = new DoubleAnimation(Opacity, 0, TimeSpan.FromMilliseconds(190))
        {
            BeginTime = TimeSpan.FromMilliseconds(180),
            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseIn }
        };
        slide.Completed += (_, _) => completion.TrySetResult();

        CardTranslate.BeginAnimation(TranslateTransform.XProperty, slide);
        CardScale.BeginAnimation(ScaleTransform.ScaleXProperty, scale);
        CardScale.BeginAnimation(ScaleTransform.ScaleYProperty, scale);
        BeginAnimation(OpacityProperty, fade);
        return completion.Task;
    }

    public void ResetAnimations()
    {
        CardTranslate.BeginAnimation(TranslateTransform.XProperty, null);
        CardScale.BeginAnimation(ScaleTransform.ScaleXProperty, null);
        CardScale.BeginAnimation(ScaleTransform.ScaleYProperty, null);
        BeginAnimation(OpacityProperty, null);
    }

    protected override void OnMouseLeftButtonUp(MouseButtonEventArgs e)
    {
        base.OnMouseLeftButtonUp(e);
        WindowSelected?.Invoke(this, Snapshot);
    }

    public event EventHandler<WindowSnapshot>? WindowSelected;
}
