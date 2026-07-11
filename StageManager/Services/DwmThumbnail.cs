using StageManager.Interop;

namespace StageManager.Services;

internal sealed class DwmThumbnail : IDisposable
{
    private nint _handle;

    private DwmThumbnail(nint handle)
    {
        _handle = handle;
    }

    public static DwmThumbnail? TryCreate(nint destination, nint source)
    {
        var result = NativeMethods.DwmRegisterThumbnail(destination, source, out var thumbnail);
        return result == 0 && thumbnail != 0 ? new DwmThumbnail(thumbnail) : null;
    }

    public void Update(NativeMethods.Rect availableBounds, NativeMethods.Rect clipBounds, byte opacity)
    {
        if (_handle == 0 || availableBounds.Width <= 0 || availableBounds.Height <= 0 || opacity == 0)
        {
            SetVisible(false);
            return;
        }

        if (NativeMethods.DwmQueryThumbnailSourceSize(_handle, out var sourceSize) != 0 ||
            sourceSize.Width <= 0 || sourceSize.Height <= 0)
        {
            return;
        }

        var scale = Math.Min(
            (double)availableBounds.Width / sourceSize.Width,
            (double)availableBounds.Height / sourceSize.Height);
        var width = Math.Max(1, (int)Math.Round(sourceSize.Width * scale));
        var height = Math.Max(1, (int)Math.Round(sourceSize.Height * scale));
        var left = availableBounds.Left + (availableBounds.Width - width) / 2;
        var top = availableBounds.Top + (availableBounds.Height - height) / 2;

        var fullDestination = new NativeMethods.Rect
        {
            Left = left,
            Top = top,
            Right = left + width,
            Bottom = top + height
        };
        var clippedDestination = Intersect(fullDestination, clipBounds);
        if (clippedDestination.Width <= 0 || clippedDestination.Height <= 0)
        {
            SetVisible(false);
            return;
        }

        var source = new NativeMethods.Rect
        {
            Left = Math.Clamp((int)Math.Round((clippedDestination.Left - fullDestination.Left) / scale), 0, sourceSize.Width),
            Top = Math.Clamp((int)Math.Round((clippedDestination.Top - fullDestination.Top) / scale), 0, sourceSize.Height),
            Right = Math.Clamp((int)Math.Round((clippedDestination.Right - fullDestination.Left) / scale), 0, sourceSize.Width),
            Bottom = Math.Clamp((int)Math.Round((clippedDestination.Bottom - fullDestination.Top) / scale), 0, sourceSize.Height)
        };

        var properties = new NativeMethods.DwmThumbnailProperties
        {
            Flags = NativeMethods.DwmThumbnailFlags.RectDestination |
                    NativeMethods.DwmThumbnailFlags.RectSource |
                    NativeMethods.DwmThumbnailFlags.Opacity |
                    NativeMethods.DwmThumbnailFlags.Visible |
                    NativeMethods.DwmThumbnailFlags.SourceClientAreaOnly,
            Destination = clippedDestination,
            Source = source,
            Opacity = opacity,
            Visible = true,
            SourceClientAreaOnly = false
        };

        NativeMethods.DwmUpdateThumbnailProperties(_handle, ref properties);
    }

    public void SetVisible(bool visible)
    {
        if (_handle == 0)
        {
            return;
        }

        var properties = new NativeMethods.DwmThumbnailProperties
        {
            Flags = NativeMethods.DwmThumbnailFlags.Visible,
            Visible = visible
        };
        NativeMethods.DwmUpdateThumbnailProperties(_handle, ref properties);
    }

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
