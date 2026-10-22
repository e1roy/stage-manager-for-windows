using StageManager.Interop;

namespace StageManager.Services;

internal sealed class DwmThumbnail : IDisposable
{
    private static int _updates;
    private static int _resizes;
    private nint _handle;
    private NativeMethods.DwmThumbnailProperties _applied;
    private bool _hasApplied;

    private DwmThumbnail(nint handle)
    {
        _handle = handle;
    }

    public static DwmThumbnail? TryCreate(nint destination, nint source)
    {
        var result = NativeMethods.DwmRegisterThumbnail(destination, source, out var thumbnail);
        if (result != 0 || thumbnail == 0)
        {
            return null;
        }

        var created = new DwmThumbnail(thumbnail);
        created.SetVisible(false);
        return created;
    }

    /// <summary>
    /// Draws the cropped source window exactly into <paramref name="destination"/>,
    /// optionally clipped to <paramref name="clip"/>. Both rectangles are in
    /// device pixels relative to the destination window.
    /// </summary>
    public void Show(NativeMethods.Rect destination, ThumbnailCrop crop, double opacity, NativeMethods.Rect? clip = null)
    {
        if (!TryGetSourceSize(out var sourceSize))
        {
            SetVisible(false);
            return;
        }

        var source = crop.Apply(sourceSize);
        // The crop is empty for every window measured so far, and asking for the
        // whole source explicitly is not the same thing as asking for none.
        if (source.Left == 0 && source.Top == 0 &&
            source.Right == sourceSize.Width && source.Bottom == sourceSize.Height)
        {
            ShowRegion(destination, null, opacity, clip);
            return;
        }

        ShowRegion(destination, source, opacity, clip);
    }

    public bool TryGetSourceSize(out NativeMethods.Size size)
    {
        size = default;
        return _handle != 0 &&
               NativeMethods.DwmQueryThumbnailSourceSize(_handle, out size) == 0 &&
               size.Width > 0 &&
               size.Height > 0;
    }

    /// <summary>
    /// Draws <paramref name="source"/>, in source-window pixels, into
    /// <paramref name="destination"/>. A null source asks DWM for the whole window,
    /// which is the only way to get its visible frame landed where the frame belongs:
    /// an explicit rectangle is measured from the window's corner, while the size
    /// DWM reports is the frame's, so passing the frame's own size shifts the
    /// picture right and down by the frame inset. DWM only means the whole window
    /// by a null source while this thumbnail has never been given one: afterwards
    /// it keeps drawing the last source rectangle it was given. A clip has to spell
    /// the whole window out after all, so it is moved by <paramref name="frameOrigin"/>,
    /// see <see cref="WindowGeometry.GetFrameOrigin"/>.
    /// </summary>
    public void ShowRegion(
        NativeMethods.Rect destination,
        NativeMethods.Rect? source,
        double opacity,
        NativeMethods.Rect? clip = null,
        System.Drawing.Point frameOrigin = default)
    {
        var alpha = (byte)Math.Clamp((int)Math.Round(opacity * 255), 0, 255);
        if (_handle == 0 || destination.Width <= 0 || destination.Height <= 0 || alpha == 0)
        {
            SetVisible(false);
            return;
        }

        if (clip is { } clipBounds)
        {
            var clipped = Intersect(destination, clipBounds);
            if (clipped.Width <= 0 || clipped.Height <= 0)
            {
                SetVisible(false);
                return;
            }

            if (source is not { } current)
            {
                // The clip has to be expressed in source pixels, so the whole
                // window is spelled out here even though DWM would use it anyway.
                if (!TryGetSourceSize(out var size))
                {
                    SetVisible(false);
                    return;
                }

                current = new NativeMethods.Rect
                {
                    Left = frameOrigin.X,
                    Top = frameOrigin.Y,
                    Right = frameOrigin.X + size.Width,
                    Bottom = frameOrigin.Y + size.Height
                };
            }

            var scaleX = (double)current.Width / destination.Width;
            var scaleY = (double)current.Height / destination.Height;
            source = new NativeMethods.Rect
            {
                Left = current.Left + (int)Math.Round((clipped.Left - destination.Left) * scaleX),
                Top = current.Top + (int)Math.Round((clipped.Top - destination.Top) * scaleY),
                Right = current.Right - (int)Math.Round((destination.Right - clipped.Right) * scaleX),
                Bottom = current.Bottom - (int)Math.Round((destination.Bottom - clipped.Bottom) * scaleY)
            };
            destination = clipped;
        }

        var flags = NativeMethods.DwmThumbnailFlags.RectDestination |
                    NativeMethods.DwmThumbnailFlags.Opacity |
                    NativeMethods.DwmThumbnailFlags.Visible;
        if (source is { } rect && rect.Width > 0 && rect.Height > 0)
        {
            flags |= NativeMethods.DwmThumbnailFlags.RectSource;
        }
        else if (source is not null)
        {
            SetVisible(false);
            return;
        }

        Apply(new NativeMethods.DwmThumbnailProperties
        {
            Flags = flags,
            Destination = destination,
            Source = source ?? default,
            Opacity = alpha,
            Visible = true,
            SourceClientAreaOnly = false
        });
    }

    public void SetVisible(bool visible)
    {
        if (_handle == 0 || (_hasApplied && _applied.Visible == visible))
        {
            return;
        }

        var properties = new NativeMethods.DwmThumbnailProperties
        {
            Flags = NativeMethods.DwmThumbnailFlags.Visible,
            Visible = visible
        };
        NativeMethods.DwmUpdateThumbnailProperties(_handle, ref properties);
        _applied.Visible = visible;
        _hasApplied = true;
    }

    private void Apply(NativeMethods.DwmThumbnailProperties properties)
    {
        // Skipping unchanged frames keeps idle cards from calling into DWM.
        if (_hasApplied &&
            _applied.Visible &&
            _applied.Flags == properties.Flags &&
            _applied.Destination == properties.Destination &&
            _applied.Source == properties.Source &&
            _applied.Opacity == properties.Opacity)
        {
            return;
        }

        if (_applied.Destination.Width != properties.Destination.Width ||
            _applied.Destination.Height != properties.Destination.Height)
        {
            Interlocked.Increment(ref _resizes);
        }

        Interlocked.Increment(ref _updates);
        NativeMethods.DwmUpdateThumbnailProperties(_handle, ref properties);
        _applied = properties;
        _hasApplied = true;
    }

    /// <summary>
    /// Thumbnail updates, and how many of them changed a thumbnail's size, across
    /// the process since the last call. DWM pays for the resizes, see <see cref="SlicedThumbnail"/>.
    /// </summary>
    internal static (int Updates, int Resizes) TakeCounts() =>
        (Interlocked.Exchange(ref _updates, 0), Interlocked.Exchange(ref _resizes, 0));

    private static NativeMethods.Rect Intersect(NativeMethods.Rect first, NativeMethods.Rect second) => new()
    {
        Left = Math.Max(first.Left, second.Left),
        Top = Math.Max(first.Top, second.Top),
        Right = Math.Min(first.Right, second.Right),
        Bottom = Math.Min(first.Bottom, second.Bottom)
    };

    public void Dispose()
    {
        var handle = Interlocked.Exchange(ref _handle, 0);
        if (handle != 0)
        {
            NativeMethods.DwmUnregisterThumbnail(handle);
        }

        GC.SuppressFinalize(this);
    }
}
