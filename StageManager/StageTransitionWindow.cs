using System.Diagnostics;
using System.Drawing;
using System.Windows.Forms;
using StageManager.Interop;
using StageManager.Services;
using Dispatcher = System.Windows.Threading.Dispatcher;

namespace StageManager;

/// <summary>
/// One window flying between two places. Tilt is how far it is turned like a
/// strip card (0 = flat); its far edge leans toward <see cref="VanishingY"/>,
/// the strip's centre line in screen pixels. The shadow changes from the one
/// the window casts where it starts to the one it casts where it lands, which is
/// <see cref="ToActiveShadow"/> instead whenever the window's frame is drawn
/// active. That is only known once its activation arrives, usually as it takes off.
/// A flight that <see cref="HoldsShadow"/> starts over a real window still casting
/// its own shadow, and casts none until <see cref="StageTransitionWindow.RevealShadows"/>.
/// </summary>
internal readonly record struct FlightSpec(
    nint Source,
    RectangleF From,
    RectangleF To,
    ThumbnailCrop Crop,
    float FromOpacity = 1,
    float ToOpacity = 1,
    float FromTilt = 0,
    float ToTilt = 0,
    float VanishingY = 0,
    ShadowStyle FromShadow = default,
    ShadowStyle ToShadow = default,
    bool HoldsShadow = false,
    ShadowStyle? ToActiveShadow = null);

/// <summary>Where a flight is drawn, how far it is turned and the shadow it casts.</summary>
internal readonly record struct FlightPose(RectangleF Rect, float Tilt, ShadowStyle Shadow);

/// <summary>
/// A full-desktop, click-through layer that animates live DWM thumbnails
/// between the strip and the stage. It has no redirection bitmap, so it is
/// fully transparent and cannot flash, and DWM composes the flights at the
/// display refresh rate.
/// </summary>
internal sealed class StageTransitionWindow : NativeWindow, IDisposable
{
    private const int Slices = 24;
    // Flights move too fast for a coarser slant to show, and every slice DWM has
    // to resize costs frame rate, see SlicedThumbnail.
    private const double SliceStep = 3;
    // While a flight leaves a card, the card still shows its own shadow for a
    // frame or two, and WPF draws it on its own schedule; fading in avoids
    // doubling it. Flights over real windows hold theirs back instead.
    private const double ShadowFadeInSeconds = 0.06;
    private const int ShadowSpreadSteps = 16;

    private readonly List<Flight> _flights = [];
    private readonly Stopwatch _clock = Stopwatch.StartNew();
    private readonly ShadowSource _shadowSource = new();
    private readonly CompositionClock _frames;
    private Rectangle _bounds;
    private bool _isCovering;
    private double _startedAt;
    private double _duration;
    private double _progress;
    private double _begunAt;
    private double _shadowFade = 1;
    private int _animatedFrames;
    private FrameStats _stats;
    private TaskCompletionSource? _animation;
    private SizeF _offset;

    internal StageTransitionWindow()
    {
        _frames = new CompositionClock(Dispatcher.CurrentDispatcher, Frames_Tick);
        CreateHandle(new CreateParams
        {
            Caption = "StageBar Transition",
            Style = NativeMethods.WsPopup,
            ExStyle = (int)(NativeMethods.WsExNoRedirectionBitmap |
                            NativeMethods.WsExToolWindow |
                            NativeMethods.WsExNoActivate |
                            NativeMethods.WsExTopmost |
                            NativeMethods.WsExTransparent)
        });

        // Showing or hiding a window plays a DWM fade that would race the
        // hand-over between thumbnails and real windows. Keep the layer shown
        // for its whole life and only move it between a 1x1 spot off the
        // desktop and the area the flights need; moves are never animated.
        WindowCatalog.SetTransitionsDisabled(Handle, true);
        Park(NativeMethods.SwpShowWindow);
    }

    internal bool IsActive => _flights.Count > 0;

    /// <summary>
    /// Shows the flights at their start positions and waits for DWM to present
    /// them, so the caller can hide the real windows underneath without a gap.
    /// <paramref name="area"/> reserves extra room, e.g. for dragging.
    /// </summary>
    internal void Begin(IReadOnlyList<FlightSpec> specs, Rectangle? area = null)
    {
        CompleteAnimation();
        var previous = _flights.ToArray();
        _flights.Clear();
        _offset = SizeF.Empty;
        _bounds = MeasureBounds(specs, area);
        _begunAt = _clock.Elapsed.TotalSeconds;
        _shadowFade = 1;

        // Later registrations draw on top, so callers list the arriving window
        // last, and each shadow is registered just before its window.
        foreach (var spec in specs)
        {
            var shadow = FlightShadow.TryCreate(Handle, _shadowSource.Handle);
            if (SlicedThumbnail.TryCreate(Handle, spec.Source, Slices, SliceStep) is { } thumbnail)
            {
                if (thumbnail.TryGetSourceSize(out var source))
                {
                    // The flight rect and the size DWM reports for the window are
                    // what decide whether the thumbnail lands 1:1 or resampled.
                    TransitionTrace.Line($"flight 0x{spec.Source:X} source={source.Width}x{source.Height} " +
                               $"from={Fmt(spec.From)} to={Fmt(spec.To)}");
                }

                _flights.Add(new Flight(spec, thumbnail, shadow));
            }
            else
            {
                shadow?.Dispose();
            }
        }

        // Thumbnail placement and the move are applied in the same DWM frame.
        Render(0);
        foreach (var flight in previous)
        {
            flight.Dispose();
        }

        NativeMethods.SetWindowPos(
            Handle,
            NativeMethods.HwndTopmost,
            _bounds.X,
            _bounds.Y,
            _bounds.Width,
            _bounds.Height,
            NativeMethods.SwpNoActivate);
        _isCovering = true;
        NativeMethods.DwmFlush();
    }

    /// <summary>Moves every flight with the pointer while a card is dragged.</summary>
    internal void SetOffset(float x, float y)
    {
        _offset = new SizeF(x, y);
        Render(0);
    }

    internal IReadOnlyDictionary<nint, FlightPose> GetCurrentPoses() =>
        _flights.ToDictionary(
            static flight => flight.Spec.Source,
            static flight => new FlightPose(flight.Current, flight.Tilt, flight.ShadowStyle));

    /// <summary>Continues from the current positions towards new targets.</summary>
    internal void Retarget(IReadOnlyDictionary<nint, FlightPose> targets)
    {
        CompleteAnimation();
        for (var index = 0; index < _flights.Count; index++)
        {
            var flight = _flights[index];
            var target = targets.TryGetValue(flight.Spec.Source, out var pose)
                ? pose
                : new FlightPose(flight.Current, flight.Tilt, flight.ShadowStyle);
            _flights[index] = flight with
            {
                Spec = flight.Spec with
                {
                    From = flight.Current,
                    To = target.Rect,
                    FromOpacity = flight.Opacity,
                    FromTilt = flight.Tilt,
                    ToTilt = target.Tilt,
                    FromShadow = flight.ShadowStyle,
                    ToShadow = target.Shadow,
                    ToActiveShadow = null
                }
            };
        }

        _offset = SizeF.Empty;
    }

    internal Task AnimateAsync(TimeSpan duration)
    {
        CompleteAnimation();
        if (_flights.Count == 0)
        {
            return Task.CompletedTask;
        }

        _animation = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        _startedAt = _clock.Elapsed.TotalSeconds;
        _duration = Math.Max(0.016, duration.TotalSeconds);
        _animatedFrames = 0;
        _stats = new FrameStats { LastTickAt = _startedAt };
        _frames.TakeCounts();
        DwmThumbnail.TakeCounts();
        _frames.Start();

        return _animation.Task;
    }

    /// <summary>
    /// Lets DWM present <paramref name="frames"/> frames while the flights'
    /// shadows fade out, handing over to the shadows that the real windows and
    /// cards underneath draw themselves.
    /// </summary>
    internal async Task FadeOutShadowsAsync(int frames)
    {
        for (var frame = 1; frame <= frames; frame++)
        {
            _shadowFade = 1 - (double)frame / frames;
            Render(_progress);
            await Task.Run(static () => NativeMethods.DwmFlush());
        }
    }

    /// <summary>
    /// Gives the held flights their shadows, all at once. Call it the moment the
    /// real windows have left the desktop: their own shadows go with them in the
    /// next composited frame, and ours has to arrive in that same frame or the
    /// two either stack or leave a gap.
    /// </summary>
    /// <remarks>
    /// Safe to call from a worker while the UI thread leaves this layer alone and
    /// no animation runs, so the call does not wait for the dispatcher.
    /// </remarks>
    internal void RevealShadows() => SetShadowsHidden(static _ => true, false);

    /// <summary>
    /// Drops the shadows of the flights of <paramref name="sources"/> without
    /// touching the flights themselves. Call it the moment those windows are home:
    /// Windows draws a landed window's own shadow from its first composited frame
    /// there, so keeping ours for even a frame stacks the two into a dark halo
    /// that then thins out.
    /// </summary>
    /// <remarks>Safe to call from a worker, like <see cref="RevealShadows"/>.</remarks>
    internal void CutShadows(IReadOnlyCollection<nint> sources) => SetShadowsHidden(sources.Contains, true);

    internal void End()
    {
        CompleteAnimation();
        foreach (var flight in _flights)
        {
            flight.Dispose();
        }

        _flights.Clear();
        if (_isCovering)
        {
            Park(0);
            _isCovering = false;
        }
    }

    public void Dispose()
    {
        End();
        DestroyHandle();
        _shadowSource.Dispose();
    }

    /// <summary>
    /// Covers only what the flights pass through. A topmost window spanning a
    /// whole monitor counts as a full-screen app to the shell, which makes the
    /// taskbar and app bars rearrange themselves on every switch.
    /// </summary>
    private static Rectangle MeasureBounds(IReadOnlyList<FlightSpec> specs, Rectangle? area)
    {
        var bounds = area ?? Rectangle.Empty;
        foreach (var spec in specs)
        {
            var travel = Rectangle.Round(RectangleF.Union(spec.From, spec.To));
            var shadow = (int)Math.Ceiling(Math.Max(spec.FromShadow.Extent, spec.ToShadow.Extent));
            travel.Inflate(shadow, shadow);
            bounds = bounds.IsEmpty ? travel : Rectangle.Union(bounds, travel);
        }

        bounds.Inflate(2, 2);
        foreach (var screen in Screen.AllScreens)
        {
            if (bounds.Contains(screen.Bounds))
            {
                bounds = Rectangle.FromLTRB(
                    Math.Max(bounds.Left, screen.Bounds.Left + 1),
                    Math.Max(bounds.Top, screen.Bounds.Top + 1),
                    bounds.Right,
                    bounds.Bottom);
            }
        }

        return bounds;
    }

    private void Park(uint extraFlags)
    {
        var desktop = SystemInformation.VirtualScreen;
        NativeMethods.SetWindowPos(
            Handle,
            NativeMethods.HwndTopmost,
            desktop.Left - 16,
            desktop.Top - 16,
            1,
            1,
            NativeMethods.SwpNoActivate | extraFlags);
    }

    protected override void WndProc(ref Message message)
    {
        switch (message.Msg)
        {
            case NativeMethods.WmNcHitTest:
                message.Result = NativeMethods.HtTransparent;
                return;
            case NativeMethods.WmMouseActivate:
                message.Result = NativeMethods.MaNoActivate;
                return;
            default:
                base.WndProc(ref message);
                return;
        }
    }

    private void SetShadowsHidden(Func<nint, bool> applies, bool hidden)
    {
        for (var index = 0; index < _flights.Count; index++)
        {
            if (applies(_flights[index].Spec.Source))
            {
                _flights[index] = _flights[index] with { IsShadowHidden = hidden };
            }
        }

        Render(_progress);
    }

    private void Frames_Tick()
    {
        var tickAt = _clock.Elapsed.TotalSeconds;
        var latency = _frames.TickLatency;
        var progress = (tickAt - _startedAt) / _duration;
        Render(Easing.CriticallyDamped(progress));
        _animatedFrames++;
        var rendered = (_clock.Elapsed.TotalSeconds - tickAt) * 1000;
        _stats.RenderTotal += rendered;
        _stats.RenderMax = Math.Max(_stats.RenderMax, rendered);
        _stats.LatencyTotal += latency;
        _stats.LatencyMax = Math.Max(_stats.LatencyMax, latency);
        var gap = (tickAt - _stats.LastTickAt) * 1000;
        if (gap > _stats.GapMax)
        {
            _stats.GapMax = gap;
            _stats.GapAt = progress;
        }

        _stats.LastTickAt = tickAt;
        if (progress >= 1)
        {
            CompleteAnimation();
        }
    }

    private void CompleteAnimation()
    {
        _frames.Stop();
        var animation = _animation;
        _animation = null;
        if (animation is not null)
        {
            // DWM slows down when it has many thumbnails to resize; this is
            // what a flight really got, against the display's refresh rate.
            // Flushes are the compositions DWM managed, skipped the ones the UI
            // thread was too busy for, latency how late a tick ran after its
            // composition, and resizes what DWM was asked to rescale per frame.
            var elapsed = _clock.Elapsed.TotalSeconds - _startedAt;
            var frames = Math.Max(_animatedFrames, 1);
            var (flushes, skipped) = _frames.TakeCounts();
            var (updates, resizes) = DwmThumbnail.TakeCounts();
            TransitionTrace.Line($"animate flights={_flights.Count} frames={_animatedFrames} " +
                                 $"in {elapsed * 1000:F0}ms ({_animatedFrames / Math.Max(elapsed, 0.001):F0} fps) " +
                                 $"flushes={flushes} skipped={skipped} gap.max={_stats.GapMax:F1}ms@{_stats.GapAt:F2} " +
                                 $"render={_stats.RenderTotal / frames:F2}/{_stats.RenderMax:F2}ms " +
                                 $"latency={_stats.LatencyTotal / frames:F2}/{_stats.LatencyMax:F2}ms " +
                                 $"updates/frame={(double)updates / frames:F0} resizes/frame={(double)resizes / frames:F0}");
        }

        animation?.TrySetResult();
    }

    private void Render(double progress)
    {
        _progress = progress;
        var fadeIn = Math.Clamp((_clock.Elapsed.TotalSeconds - _begunAt) / ShadowFadeInSeconds, 0, 1);
        for (var index = 0; index < _flights.Count; index++)
        {
            var flight = _flights[index];
            var shadowPresence = (flight.IsShadowHidden ? 0 : flight.Spec.HoldsShadow ? 1 : fadeIn) * _shadowFade;
            var from = flight.Spec.From;
            from.Offset(_offset.Width, _offset.Height);
            var current = Lerp(from, flight.Spec.To, (float)progress);
            var opacity = flight.Spec.FromOpacity + (flight.Spec.ToOpacity - flight.Spec.FromOpacity) * (float)progress;
            var tilt = flight.Spec.FromTilt + (flight.Spec.ToTilt - flight.Spec.FromTilt) * (float)progress;
            var toShadow = flight.Spec.ToActiveShadow is { } activeShadow && WindowCatalog.HasActiveFrame(flight.Spec.Source)
                ? activeShadow
                : flight.Spec.ToShadow;
            // A new blur spread resizes every corner piece of the shadow, which
            // DWM pays for each frame; steps this small do not show in motion.
            var spreadProgress = (float)(Math.Round(progress * ShadowSpreadSteps) / ShadowSpreadSteps);
            var spread = ShadowStyle.Lerp(flight.Spec.FromShadow, toShadow, spreadProgress);
            var shadow = ShadowStyle.Lerp(flight.Spec.FromShadow, toShadow, (float)progress) with
            {
                AmbientSigma = spread.AmbientSigma,
                KeySigma = spread.KeySigma
            };
            _flights[index] = flight with { Current = current, Opacity = opacity, Tilt = tilt, ShadowStyle = shadow };
            // The shadow fades faster than the window, so its edges never show
            // through a window that is fading away.
            flight.Shadow?.Show(
                current.Left - _bounds.X,
                current.Top - _bounds.Y,
                current.Width,
                current.Height,
                tilt,
                flight.Spec.VanishingY - _bounds.Y,
                shadow,
                opacity * opacity * shadowPresence);
            flight.Thumbnail.Show(
                current.Left - _bounds.X,
                current.Top - _bounds.Y,
                current.Width,
                current.Height,
                flight.Spec.Crop,
                tilt,
                flight.Spec.VanishingY - _bounds.Y,
                opacity);
        }
    }

    private static string Fmt(RectangleF rect) =>
        $"{rect.Left:F0},{rect.Top:F0} {rect.Width:F0}x{rect.Height:F0}";

    private static RectangleF Lerp(RectangleF from, RectangleF to, float progress) => RectangleF.FromLTRB(
        from.Left + (to.Left - from.Left) * progress,
        from.Top + (to.Top - from.Top) * progress,
        from.Right + (to.Right - from.Right) * progress,
        from.Bottom + (to.Bottom - from.Bottom) * progress);

    /// <summary>What one animation's frames cost, for the transition log. Times in milliseconds.</summary>
    private struct FrameStats
    {
        internal double LastTickAt;
        internal double GapMax;
        // Linear progress of the flight when the longest gap ended.
        internal double GapAt;
        internal double RenderTotal;
        internal double RenderMax;
        internal double LatencyTotal;
        internal double LatencyMax;
    }

    private sealed record Flight(FlightSpec Spec, SlicedThumbnail Thumbnail, FlightShadow? Shadow)
    {
        internal RectangleF Current { get; init; } = Spec.From;

        internal float Opacity { get; init; } = Spec.FromOpacity;

        internal float Tilt { get; init; } = Spec.FromTilt;

        internal ShadowStyle ShadowStyle { get; init; } = Spec.FromShadow;

        internal bool IsShadowHidden { get; init; } = Spec.HoldsShadow;

        internal void Dispose()
        {
            Shadow?.Dispose();
            Thumbnail.Dispose();
        }
    }
}
