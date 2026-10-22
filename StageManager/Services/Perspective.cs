namespace StageManager.Services;

/// <summary>
/// The look of the macOS strip, where every card is turned away from the
/// viewer: the far (right) edge is shorter than the near one and leans toward
/// the strip's horizontal centre line, and the card is narrower than the window
/// it shows. DWM thumbnails can only be rectangles, so cards are drawn as thin
/// vertical slices whose height follows this curve.
/// </summary>
internal static class Perspective
{
    /// <summary>Height of a fully turned card's far edge relative to its near edge.</summary>
    private const double FarEdgeScale = 0.84;

    /// <summary>How strongly the far edge leans toward the vanishing line.</summary>
    private const double Convergence = 0.4;

    /// <summary>
    /// Width of a fully turned card relative to the window facing the viewer,
    /// measured on macOS: a window turned into the strip loses about 30 percent.
    /// </summary>
    private const double TurnedWidthScale = 0.7;

    /// <summary>
    /// Height scale of the column at screen <paramref name="position"/> (0 = near
    /// edge, 1 = far edge) for a card turned by <paramref name="tilt"/> (0 = flat, 1 = fully turned).
    /// Edges of a turned plane stay straight, so the height changes linearly.
    /// </summary>
    internal static double Scale(double position, double tilt) =>
        1 - (1 - FarEdge(tilt)) * position;

    /// <summary>
    /// Where a column at <paramref name="sourcePosition"/> across the window lands
    /// across the turned card, both from 0 at the near edge to 1 at the far one.
    /// The near half is closer to the viewer and so takes up more of the card.
    /// </summary>
    internal static double ScreenPosition(double sourcePosition, double tilt)
    {
        var farEdge = FarEdge(tilt);
        return sourcePosition / (farEdge + (1 - farEdge) * sourcePosition);
    }

    /// <summary>The inverse of <see cref="ScreenPosition"/>: which column of the window lands at <paramref name="screenPosition"/>.</summary>
    internal static double SourcePosition(double screenPosition, double tilt)
    {
        var farEdge = FarEdge(tilt);
        return screenPosition * farEdge / (1 - (1 - farEdge) * screenPosition);
    }

    /// <summary>How much narrower a card turned by <paramref name="tilt"/> is than its window.</summary>
    internal static double WidthScale(double tilt) => 1 - (1 - TurnedWidthScale) * tilt;

    /// <summary>Vertical centre of a column whose height has been scaled by <paramref name="scale"/>.</summary>
    internal static double CenterY(double centerY, double vanishingY, double scale) =>
        centerY + (vanishingY - centerY) * (1 - scale) * Convergence;

    /// <summary>The four corners of a turned card, clockwise from the top left.</summary>
    internal static (double X, double Y)[] Corners(
        double left,
        double top,
        double width,
        double height,
        double tilt,
        double vanishingY)
    {
        var farScale = Scale(1, tilt);
        var farCenter = CenterY(top + height / 2, vanishingY, farScale);
        var farHalf = height * farScale / 2;
        return
        [
            (left, top),
            (left + width, farCenter - farHalf),
            (left + width, farCenter + farHalf),
            (left, top + height)
        ];
    }

    private static double FarEdge(double tilt) => 1 - (1 - FarEdgeScale) * tilt;
}
