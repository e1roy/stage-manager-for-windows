using System.Drawing;
using System.Drawing.Drawing2D;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Windows.Forms;
using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// A still picture of a window, held by a layered window far off the desktop.
/// Cards resting in the strip cut their DWM thumbnails from it instead of from
/// the live window, so DWM no longer redraws the strip whenever the window
/// presents a frame, and no thumbnail keeps a parked window counted as visible.
/// </summary>
internal sealed class WindowStill : NativeWindow, IDisposable
{
    private WindowStill(Bitmap picture)
    {
        CreateHandle(new CreateParams
        {
            Caption = "StageBar Still",
            Style = NativeMethods.WsPopup | NativeMethods.WsVisible,
            ExStyle = (int)(NativeMethods.WsExLayered |
                            NativeMethods.WsExToolWindow |
                            NativeMethods.WsExNoActivate |
                            NativeMethods.WsExTransparent)
        });

        try
        {
            WindowCatalog.SetTransitionsDisabled(Handle, true);
            Paint(picture);
        }
        catch
        {
            DestroyHandle();
            throw;
        }
    }

    /// <summary>
    /// Takes a still of <paramref name="window"/>'s visible frame, at most
    /// <paramref name="maximumHeight"/> device pixels tall. The capture runs on a
    /// worker, because a window that stops responding can hold it up.
    /// </summary>
    /// <returns><c>null</c> for a minimized window, or one that could not be captured.</returns>
    internal static async Task<WindowStill?> CaptureAsync(nint window, int maximumHeight)
    {
        using var picture = await Task.Run(() => Capture(window, maximumHeight));
        if (picture is null)
        {
            return null;
        }

        try
        {
            return new WindowStill(picture);
        }
        catch (ExternalException)
        {
            return null;
        }
    }

    public void Dispose() => DestroyHandle();

    private static Bitmap? Capture(nint window, int maximumHeight)
    {
        // A minimized window has no picture to capture; DWM's own thumbnail of
        // it already shows the last frame without updating.
        if (maximumHeight <= 0 || !NativeMethods.IsWindow(window) || NativeMethods.IsIconic(window))
        {
            return null;
        }

        var bounds = WindowGeometry.GetBounds(window);
        var frame = WindowGeometry.GetFrameBounds(window);
        // DWM thumbnails show the visible frame, not the window rectangle around
        // it, so the still is cut to the same, see WindowGeometry.Measure.
        var visible = Rectangle.Intersect(
            new Rectangle(frame.X - bounds.X, frame.Y - bounds.Y, frame.Width, frame.Height),
            new Rectangle(Point.Empty, bounds.Size));
        if (visible.Width <= 0 || visible.Height <= 0)
        {
            return null;
        }

        Bitmap? picture = null;
        try
        {
            using var whole = new Bitmap(bounds.Width, bounds.Height, PixelFormat.Format32bppRgb);
            using (var graphics = Graphics.FromImage(whole))
            {
                var dc = graphics.GetHdc();
                try
                {
                    // Full content includes what the window draws with DirectX, such as browsers and video.
                    if (!NativeMethods.PrintWindow(window, dc, NativeMethods.PwRenderFullContent))
                    {
                        return null;
                    }
                }
                finally
                {
                    graphics.ReleaseHdc(dc);
                }
            }

            // Cards never draw a window larger than this, so a full-size still
            // would only hold more memory and leave DWM more to shrink.
            using var frameOnly = whole.Clone(visible, PixelFormat.Format32bppRgb);
            var height = Math.Min(visible.Height, maximumHeight);
            var width = Math.Max(1, (int)Math.Round(visible.Width * (double)height / visible.Height));
            picture = new Bitmap(width, height, PixelFormat.Format32bppPArgb);
            using (var graphics = Graphics.FromImage(picture))
            using (var attributes = new ImageAttributes())
            {
                // Mirroring at the edges keeps resampling from darkening them.
                attributes.SetWrapMode(WrapMode.TileFlipXY);
                graphics.CompositingMode = CompositingMode.SourceCopy;
                graphics.InterpolationMode = InterpolationMode.HighQualityBicubic;
                graphics.PixelOffsetMode = PixelOffsetMode.HighQuality;
                graphics.DrawImage(
                    frameOnly,
                    new Rectangle(0, 0, width, height),
                    0,
                    0,
                    frameOnly.Width,
                    frameOnly.Height,
                    GraphicsUnit.Pixel,
                    attributes);
            }

            if (IsBlank(picture))
            {
                picture.Dispose();
                return null;
            }

            return picture;
        }
        catch (Exception exception) when (exception is ArgumentException or ExternalException or OutOfMemoryException)
        {
            picture?.Dispose();
            return null;
        }
    }

    /// <summary>All black, which is how PrintWindow fails for windows it cannot draw.</summary>
    private static bool IsBlank(Bitmap picture)
    {
        var data = picture.LockBits(
            new Rectangle(0, 0, picture.Width, picture.Height),
            ImageLockMode.ReadOnly,
            PixelFormat.Format32bppPArgb);
        try
        {
            var row = new int[picture.Width];
            for (var y = 0; y < picture.Height; y++)
            {
                Marshal.Copy(data.Scan0 + y * data.Stride, row, 0, row.Length);
                if (row.Any(static pixel => (pixel & 0xFFFFFF) != 0))
                {
                    return false;
                }
            }

            return true;
        }
        finally
        {
            picture.UnlockBits(data);
        }
    }

    private void Paint(Bitmap picture)
    {
        var screenDc = NativeMethods.GetDC(0);
        var memoryDc = NativeMethods.CreateCompatibleDC(screenDc);
        var hBitmap = picture.GetHbitmap(Color.FromArgb(0));
        var previous = NativeMethods.SelectObject(memoryDc, hBitmap);
        try
        {
            // Well outside any monitor, like the shadow source; DWM still renders it for thumbnails.
            var desktop = SystemInformation.VirtualScreen;
            var position = new NativeMethods.Point
            {
                X = desktop.Left - picture.Width - 20000,
                Y = desktop.Top - picture.Height - 20000
            };
            var size = new NativeMethods.Size { Width = picture.Width, Height = picture.Height };
            var origin = new NativeMethods.Point();
            var blend = new NativeMethods.BlendFunction
            {
                BlendOp = NativeMethods.AcSrcOver,
                SourceConstantAlpha = 255,
                AlphaFormat = NativeMethods.AcSrcAlpha
            };
            NativeMethods.UpdateLayeredWindow(Handle, screenDc, ref position, ref size, memoryDc, ref origin, 0, ref blend, NativeMethods.UlwAlpha);
        }
        finally
        {
            NativeMethods.SelectObject(memoryDc, previous);
            NativeMethods.DeleteObject(hBitmap);
            NativeMethods.DeleteDC(memoryDc);
            NativeMethods.ReleaseDC(0, screenDc);
        }
    }
}
