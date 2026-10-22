using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// Undoes the transparent, click-through style older builds left on windows they
/// hid for a flight. Forcing that style on an application that renders through
/// DirectComposition leaves it painting nothing, so it is only ever cleared here.
/// </summary>
internal static class WindowPriming
{
    private static readonly HashSet<nint> Hidden = [];

    /// <summary>
    /// Gives the window back: opaque, and without the layered style. Applications
    /// that render through DirectComposition never paint into a window that was
    /// forced layered, so leaving the style set hands the user a blank window.
    /// </summary>
    internal static void Reveal(nint window)
    {
        var isOurs = Hidden.Remove(window);
        if (!NativeMethods.IsWindow(window))
        {
            return;
        }

        var style = NativeMethods.GetWindowLongPtr(window, NativeMethods.GwlExStyle).ToInt64();
        if ((style & NativeMethods.WsExLayered) == 0 || (!isOurs && !IsHiddenTransparent(window)))
        {
            return;
        }

        NativeMethods.SetLayeredWindowAttributes(window, 0, 255, NativeMethods.LwaAlpha);
        NativeMethods.SetWindowLongPtr(
            window,
            NativeMethods.GwlExStyle,
            (nint)(style & ~(NativeMethods.WsExLayered | NativeMethods.WsExTransparent)));
        // The style change only reaches the frame on the next SetWindowPos.
        NativeMethods.SetWindowPos(
            window,
            0,
            0,
            0,
            0,
            0,
            NativeMethods.SwpFrameChanged |
            NativeMethods.SwpNoMove |
            NativeMethods.SwpNoSize |
            NativeMethods.SwpNoZOrder |
            NativeMethods.SwpNoActivate);
    }

    /// <summary>Reveals everything still hidden; the safety net for exits and failures.</summary>
    internal static void RevealAll()
    {
        foreach (var window in Hidden.ToArray())
        {
            Reveal(window);
        }
    }

    /// <summary>
    /// A window left at zero alpha and click-through is one of ours even when the
    /// process that hid it lost track of it, for example after a crash.
    /// </summary>
    private static bool IsHiddenTransparent(nint window) =>
        (NativeMethods.GetWindowLongPtr(window, NativeMethods.GwlExStyle).ToInt64() &
         NativeMethods.WsExTransparent) != 0 &&
        NativeMethods.GetLayeredWindowAttributes(window, out _, out var alpha, out var flags) &&
        (flags & NativeMethods.LwaAlpha) != 0 &&
        alpha == 0;
}
