using System.Windows;
using System.Windows.Controls;
using System.Windows.Media.Animation;

namespace StageManager.Services;

internal sealed class ScrollOffsetAnimator : Animatable
{
    private static readonly DependencyProperty OffsetProperty = DependencyProperty.Register(
        nameof(Offset),
        typeof(double),
        typeof(ScrollOffsetAnimator),
        new PropertyMetadata(0d, OnOffsetChanged));

    private readonly ScrollViewer _scrollViewer;

    public ScrollOffsetAnimator(ScrollViewer scrollViewer)
    {
        _scrollViewer = scrollViewer;
    }

    private double Offset
    {
        get => (double)GetValue(OffsetProperty);
        set => SetValue(OffsetProperty, value);
    }

    public void AnimateTo(double target)
    {
        BeginAnimation(OffsetProperty, null);
        Offset = _scrollViewer.VerticalOffset;

        var animation = new DoubleAnimation(Offset, target, TimeSpan.FromMilliseconds(330))
        {
            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut },
            FillBehavior = FillBehavior.Stop
        };
        animation.Completed += (_, _) =>
        {
            BeginAnimation(OffsetProperty, null);
            Offset = target;
        };
        BeginAnimation(OffsetProperty, animation, HandoffBehavior.SnapshotAndReplace);
    }

    private static void OnOffsetChanged(DependencyObject dependencyObject, DependencyPropertyChangedEventArgs args)
    {
        var animator = (ScrollOffsetAnimator)dependencyObject;
        animator._scrollViewer.ScrollToVerticalOffset((double)args.NewValue);
    }

    protected override Freezable CreateInstanceCore() => new ScrollOffsetAnimator(_scrollViewer);
}
