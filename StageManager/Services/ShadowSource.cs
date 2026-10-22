using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Windows.Forms;
using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// A layered window far off the desktop holding one blurred black square.
/// Flight shadows are DWM thumbnails cut from it, so DWM composes a shadow in
/// the same frame as the window it belongs to and the two never drift apart.
/// </summary>
internal sealed class ShadowSource : NativeWindow, IDisposable
{
    /// <summary>Spread of the rendered blur; pieces are stretched from it to any other spread.</summary>
    internal const int Sigma = 20;

    /// <summary>How far the blur reaches from an edge of the square, both outward and inward.</summary>
    internal const int Reach = 3 * Sigma;

    /// <summary>Leaves an inner span wider than two reaches, so the corners are independent.</summary>
    internal const int Size = 4 * Reach + 1;

    /// <summary>A row and column where the blur no longer varies along the edge.</summary>
    internal const int Middle = Size / 2;

    internal ShadowSource()
    {
        CreateHandle(new CreateParams
        {
            Caption = "StageBar Shadow",
            Style = NativeMethods.WsPopup | NativeMethods.WsVisible,
            ExStyle = (int)(NativeMethods.WsExLayered |
                            NativeMethods.WsExToolWindow |
                            NativeMethods.WsExNoActivate |
                            NativeMethods.WsExTransparent)
        });

        WindowCatalog.SetTransitionsDisabled(Handle, true);
        Paint();
    }

    public void Dispose() => DestroyHandle();

    private void Paint()
    {
        using var bitmap = new Bitmap(Size, Size, PixelFormat.Format32bppPArgb);
        var data = bitmap.LockBits(new Rectangle(0, 0, Size, Size), ImageLockMode.WriteOnly, PixelFormat.Format32bppPArgb);
        try
        {
            // A blurred rectangle is separable: its alpha is the product of
            // one blurred edge pair across and one down.
            var profile = new double[Size];
            for (var index = 0; index < Size; index++)
            {
                var center = index + 0.5;
                profile[index] = Gaussian.Cdf((center - Reach) / Sigma) - Gaussian.Cdf((center - (Size - Reach)) / Sigma);
            }

            var row = new byte[Size * 4];
            for (var y = 0; y < Size; y++)
            {
                for (var x = 0; x < Size; x++)
                {
                    // Black needs no premultiplication: only alpha is non-zero.
                    row[x * 4 + 3] = (byte)Math.Clamp((int)Math.Round(profile[x] * profile[y] * 255), 0, 255);
                }

                Marshal.Copy(row, 0, data.Scan0 + y * data.Stride, row.Length);
            }
        }
        finally
        {
            bitmap.UnlockBits(data);
        }

        var screenDc = NativeMethods.GetDC(0);
        var memoryDc = NativeMethods.CreateCompatibleDC(screenDc);
        var hBitmap = bitmap.GetHbitmap(Color.FromArgb(0));
        var previous = NativeMethods.SelectObject(memoryDc, hBitmap);
        try
        {
            // Well outside any monitor; DWM still renders it for thumbnails.
            var desktop = SystemInformation.VirtualScreen;
            var position = new NativeMethods.Point { X = desktop.Left - Size - 20000, Y = desktop.Top - Size - 20000 };
            var size = new NativeMethods.Size { Width = Size, Height = Size };
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

    private static class Gaussian
    {
        /// <summary>The standard normal CDF, via Abramowitz and Stegun 7.1.26 (error below 1e-7).</summary>
        internal static double Cdf(double x)
        {
            var z = Math.Abs(x) / Math.Sqrt(2);
            var t = 1 / (1 + 0.3275911 * z);
            var erf = 1 - ((((1.061405429 * t - 1.453152027) * t + 1.421413741) * t - 0.284496736) * t + 0.254829592) * t * Math.Exp(-z * z);
            return 0.5 * (1 + Math.Sign(x) * erf);
        }
    }
}
