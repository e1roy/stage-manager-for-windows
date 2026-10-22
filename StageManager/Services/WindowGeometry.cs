using System.Drawing;
using System.Runtime.InteropServices;
using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// The invisible resize border around a window, expressed as fractions of the
/// window size so it can be applied to any DWM thumbnail source size.
/// </summary>
internal readonly record struct ThumbnailCrop(double Left, double Top, double Right, double Bottom)
{
    internal NativeMethods.Rect Apply(NativeMethods.Size source) => new()
    {
        Left = (int)Math.Round(source.Width * Left),
        Top = (int)Math.Round(source.Height * Top),
        Right = (int)Math.Round(source.Width * (1 - Right)),
        Bottom = (int)Math.Round(source.Height * (1 - Bottom))
    };
}

internal readonly record struct WindowMetrics(Rectangle Bounds, Rectangle FrameBounds, ThumbnailCrop Crop);

internal static class WindowGeometry
{
    private static readonly Dictionary<nint, FrameInsets> Insets = [];

    /// <summary>
    /// Measures a window's outer bounds and its visible DWM frame. Minimized
    /// windows report the frame they will occupy once restored.
    /// </summary>
    internal static WindowMetrics Measure(nint window)
    {
        var bounds = GetBounds(window);
        if (NativeMethods.IsIconic(window))
        {
            Insets.TryGetValue(window, out var cached);
            var restored = GetRestoredFrameBounds(window, cached) ?? bounds;
            return new WindowMetrics(bounds, restored, default);
        }

        var frame = GetFrameBounds(window, bounds);
        var insets = new FrameInsets(
            Math.Max(0, frame.Left - bounds.Left),
            Math.Max(0, frame.Top - bounds.Top),
            Math.Max(0, bounds.Right - frame.Right),
            Math.Max(0, bounds.Bottom - frame.Bottom));
        Insets[window] = insets;
        // Measured on this machine: the size DWM reports for a thumbnail is the
        // window's visible frame, not its window rectangle — for eleven windows of
        // every kind the two agreed to the pixel, while the window rectangle was
        // 10 to 24 device pixels larger. Cropping the frame off the source therefore
        // cropped the frame itself and stretched what was left by up to a percent,
        // which is the "was the width stretched?" jump when a flight lands. The
        // thumbnail already is the frame, so it is drawn whole.
        return new WindowMetrics(bounds, frame, default);
    }

    internal static Rectangle GetBounds(nint window) =>
        NativeMethods.GetWindowRect(window, out var rect)
            ? Rectangle.FromLTRB(rect.Left, rect.Top, rect.Right, rect.Bottom)
            : Rectangle.Empty;

    internal static Rectangle GetFrameBounds(nint window) => GetFrameBounds(window, GetBounds(window));

    /// <summary>
    /// Where the visible frame starts inside the window rectangle. An explicit
    /// thumbnail source rectangle is measured from the window's corner while the
    /// size DWM reports is the frame's, so a source rectangle has to be moved by
    /// this much to show the frame where a whole-window thumbnail shows it.
    /// </summary>
    /// <returns><c>null</c> while the window is minimized and its frame is unknown.</returns>
    internal static Point? GetFrameOrigin(nint window)
    {
        if (NativeMethods.IsIconic(window))
        {
            return null;
        }

        var bounds = GetBounds(window);
        var frame = GetFrameBounds(window, bounds);
        return new Point(Math.Max(0, frame.Left - bounds.Left), Math.Max(0, frame.Top - bounds.Top));
    }

    internal static void Prune(IReadOnlySet<nint> liveWindows)
    {
        foreach (var window in Insets.Keys.Where(window => !liveWindows.Contains(window)).ToArray())
        {
            Insets.Remove(window);
        }
    }

    private static Rectangle GetFrameBounds(nint window, Rectangle fallback)
    {
        var result = NativeMethods.DwmGetWindowRectAttribute(
            window,
            NativeMethods.DwmwaExtendedFrameBounds,
            out var frame,
            Marshal.SizeOf<NativeMethods.Rect>());
        return result == 0 && frame.Width > 0 && frame.Height > 0
            ? Rectangle.FromLTRB(frame.Left, frame.Top, frame.Right, frame.Bottom)
            : fallback;
    }

    private static Rectangle? GetRestoredFrameBounds(nint window, FrameInsets insets)
    {
        var placement = new NativeMethods.WindowPlacement
        {
            Length = (uint)Marshal.SizeOf<NativeMethods.WindowPlacement>()
        };
        if (!NativeMethods.GetWindowPlacement(window, ref placement))
        {
            return null;
        }

        var normal = placement.NormalPosition;
        var monitor = NativeMethods.MonitorFromRect(ref normal, NativeMethods.MonitorDefaultToNearest);
        var info = new NativeMethods.MonitorInfo { Size = (uint)Marshal.SizeOf<NativeMethods.MonitorInfo>() };
        if (!NativeMethods.GetMonitorInfo(monitor, ref info))
        {
            return null;
        }

        if ((placement.Flags & NativeMethods.WpfRestoreToMaximized) != 0)
        {
            return Rectangle.FromLTRB(info.Work.Left, info.Work.Top, info.Work.Right, info.Work.Bottom);
        }

        // Placement uses workspace coordinates unless the window is a tool window.
        var extendedStyle = NativeMethods.GetWindowLongPtr(window, NativeMethods.GwlExStyle).ToInt64();
        var isToolWindow = (extendedStyle & NativeMethods.WsExToolWindow) != 0;
        var offsetX = isToolWindow ? 0 : info.Work.Left - info.Monitor.Left;
        var offsetY = isToolWindow ? 0 : info.Work.Top - info.Monitor.Top;
        return Rectangle.FromLTRB(
            normal.Left + offsetX + insets.Left,
            normal.Top + offsetY + insets.Top,
            normal.Right + offsetX - insets.Right,
            normal.Bottom + offsetY - insets.Bottom);
    }

    private readonly record struct FrameInsets(int Left, int Top, int Right, int Bottom);
}
