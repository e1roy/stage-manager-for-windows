using System.ComponentModel;
using System.IO;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using StageManager.Interop;
using StageManager.Models;
using DrawingIcon = System.Drawing.Icon;

namespace StageManager.Services;

internal sealed class WindowIconService
{
    // Large enough for the strip's icons to stay sharp at 200% scaling.
    private const int IconSize = 256;
    private readonly Dictionary<string, ImageSource?> _icons = new(StringComparer.Ordinal);

    internal ImageSource? GetIcon(WindowSnapshot window)
    {
        if (_icons.TryGetValue(window.AppKey, out var cached))
        {
            return cached;
        }

        var icon = LoadFromExecutable(window.ExecutablePath) ?? LoadFromWindow(window.Handle);
        _icons[window.AppKey] = icon;
        return icon;
    }

    private static ImageSource? LoadFromExecutable(string? executablePath)
    {
        if (string.IsNullOrWhiteSpace(executablePath))
        {
            return null;
        }

        try
        {
            using var icon = DrawingIcon.ExtractIcon(executablePath, 0, IconSize) ??
                             DrawingIcon.ExtractAssociatedIcon(executablePath);
            return icon is null ? null : CreateImage(icon.Handle);
        }
        catch (Exception exception) when (exception is ArgumentException or IOException or Win32Exception)
        {
            return null;
        }
    }

    private static ImageSource? LoadFromWindow(nint window)
    {
        NativeMethods.SendMessageTimeout(
            window,
            NativeMethods.WmGetIcon,
            NativeMethods.IconBig,
            0,
            NativeMethods.SmtoAbortIfHung,
            100,
            out var icon);
        if (icon == 0)
        {
            icon = NativeMethods.GetClassLongPtr(window, NativeMethods.GclpHIcon);
        }

        return icon == 0 ? null : CreateImage(icon);
    }

    private static ImageSource? CreateImage(nint icon)
    {
        try
        {
            var source = Imaging.CreateBitmapSourceFromHIcon(
                icon,
                System.Windows.Int32Rect.Empty,
                BitmapSizeOptions.FromEmptyOptions());
            source.Freeze();
            return source;
        }
        catch (Exception exception) when (exception is ArgumentException or Win32Exception or System.Runtime.InteropServices.COMException)
        {
            return null;
        }
    }
}
