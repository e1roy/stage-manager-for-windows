using System.Drawing;
using System.Runtime.InteropServices;

namespace StageManager.Services;

internal sealed class AppBarService : IDisposable
{
    private const uint AbmNew = 0x00000000;
    private const uint AbmRemove = 0x00000001;
    private const uint AbmQueryPos = 0x00000002;
    private const uint AbmSetPos = 0x00000003;
    private const uint AbeLeft = 0;

    internal const int PositionChangedNotification = 1;
    internal const int FullScreenAppNotification = 2;

    private readonly nint _window;
    private bool _registered;

    internal AppBarService(nint window)
    {
        _window = window;
        CallbackMessage = RegisterWindowMessage($"StageManager.AppBar.{Environment.ProcessId}");
    }

    internal uint CallbackMessage { get; }

    internal bool IsRegistered => _registered;

    internal Rectangle Reserve(Rectangle workingArea, int width)
    {
        Register();
        var data = CreateData();
        data.Edge = AbeLeft;
        data.Rectangle = new NativeRectangle
        {
            Left = workingArea.Left,
            Top = workingArea.Top,
            Right = workingArea.Right,
            Bottom = workingArea.Bottom
        };

        SHAppBarMessage(AbmQueryPos, ref data);
        data.Rectangle.Right = data.Rectangle.Left + width;
        SHAppBarMessage(AbmSetPos, ref data);
        return Rectangle.FromLTRB(
            data.Rectangle.Left,
            data.Rectangle.Top,
            data.Rectangle.Right,
            data.Rectangle.Bottom);
    }

    internal void Unregister()
    {
        if (!_registered)
        {
            return;
        }

        var data = CreateData();
        SHAppBarMessage(AbmRemove, ref data);
        _registered = false;
    }

    public void Dispose()
    {
        Unregister();
        GC.SuppressFinalize(this);
    }

    private void Register()
    {
        if (_registered)
        {
            return;
        }

        var data = CreateData();
        data.CallbackMessage = CallbackMessage;
        _registered = SHAppBarMessage(AbmNew, ref data) != 0;
    }

    private AppBarData CreateData() => new()
    {
        Size = (uint)Marshal.SizeOf<AppBarData>(),
        Window = _window
    };

    [DllImport("shell32.dll")]
    private static extern nuint SHAppBarMessage(uint message, ref AppBarData data);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern uint RegisterWindowMessage(string message);

    [StructLayout(LayoutKind.Sequential)]
    private struct AppBarData
    {
        internal uint Size;
        internal nint Window;
        internal uint CallbackMessage;
        internal uint Edge;
        internal NativeRectangle Rectangle;
        internal nint Parameter;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct NativeRectangle
    {
        internal int Left;
        internal int Top;
        internal int Right;
        internal int Bottom;
    }
}
