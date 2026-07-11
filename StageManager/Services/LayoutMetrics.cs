namespace StageManager.Services;

internal static class LayoutMetrics
{
    internal const double MinimumWidth = 200;
    internal const double MaximumWidth = 420;
    internal const int AbsoluteMaximumCardCount = 8;

    internal static CardLayout Calculate(double panelWidth)
    {
        var normalizedWidth = Math.Clamp(panelWidth, MinimumWidth, MaximumWidth);
        var previewWidth = Math.Max(150, normalizedWidth - 42);
        var previewHeight = Math.Clamp(previewWidth * 0.57, 88, 176);
        return new CardLayout(previewWidth, previewHeight, previewHeight + 22);
    }

    internal static int CalculateMaximumCardCount(double panelWidth, double availableHeight)
    {
        var layout = Calculate(panelWidth);
        var maximum = (int)Math.Floor(Math.Max(layout.ItemExtent, availableHeight - 32) / layout.ItemExtent);
        return Math.Clamp(maximum, 1, AbsoluteMaximumCardCount);
    }
}

internal readonly record struct CardLayout(double PreviewWidth, double PreviewHeight, double ItemExtent);
