namespace StageManager.Services;

internal static class LayoutMetrics
{
    internal const double MinimumWidth = 100;
    internal const double MaximumWidth = 420;

    /// <summary>Range of the user's icon size, relative to the size that suits the card.</summary>
    internal const double MinimumIconScale = 0.6;
    internal const double MaximumIconScale = 1.6;

    /// <summary>How far cards shrink to fit more apps before the strip scrolls.</summary>
    internal const double MinimumCardScale = 0.42;

    internal static CardLayout Calculate(double panelWidth, double iconScale = 1)
    {
        var normalizedWidth = Math.Clamp(panelWidth, MinimumWidth, MaximumWidth);
        var previewWidth = normalizedWidth - 42;
        var previewHeight = Math.Min(previewWidth * 0.57, 176);
        var iconSize = Math.Clamp(previewHeight * 0.3, 20, 56) *
                       Math.Clamp(iconScale, MinimumIconScale, MaximumIconScale);
        return new CardLayout(previewWidth, previewHeight, previewHeight + 22, iconSize);
    }
}

internal readonly record struct CardLayout(double PreviewWidth, double PreviewHeight, double ItemExtent, double IconSize);
