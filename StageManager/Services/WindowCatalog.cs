using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using StageManager.Interop;
using StageManager.Models;

namespace StageManager.Services;

internal sealed class WindowCatalog
{
    private const string ApplicationFrameClass = "ApplicationFrameWindow";
    private const string CoreWindowClass = "Windows.UI.Core.CoreWindow";

    private static readonly HashSet<string> ExcludedClasses = new(StringComparer.Ordinal)
    {
        "Progman",
        "WorkerW",
        "Shell_TrayWnd",
        "Shell_SecondaryTrayWnd",
        CoreWindowClass
    };

    private static readonly HashSet<string> DesktopClasses = new(StringComparer.Ordinal)
    {
        "Progman",
        "WorkerW"
    };

#if DEBUG
    // Development aid: limit management to the listed executables so a test
    // run cannot rearrange the developer's own windows.
    private static readonly HashSet<string>? AppFilter = Environment.GetEnvironmentVariable("STAGEBAR_APP_FILTER") is { Length: > 0 } filter
        ? new HashSet<string>(filter.Split(',', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries), StringComparer.OrdinalIgnoreCase)
        : null;

#endif
    private readonly uint _currentProcessId = (uint)Environment.ProcessId;
    private readonly Dictionary<uint, AppIdentity> _applications = [];

    /// <summary>
    /// Returns application windows in z-order, front-most first. Owned windows
    /// such as dialogs are left out because they travel with their owner.
    /// </summary>
    public IReadOnlyList<WindowSnapshot> GetWindows()
    {
        var foreground = NativeMethods.GetForegroundWindow();
        var windows = new List<WindowSnapshot>();
        var processes = new HashSet<uint>();

        NativeMethods.EnumWindows((window, _) =>
        {
            if (TryCreateSnapshot(window, foreground, processes) is { } snapshot)
            {
                windows.Add(snapshot);
            }

            return true;
        }, 0);

        foreach (var processId in _applications.Keys.Where(id => !processes.Contains(id)).ToArray())
        {
            _applications.Remove(processId);
        }

        var handles = windows.Select(static window => window.Handle).ToHashSet();
        WindowGeometry.Prune(handles);
        return windows.Where(window => !HasListedOwner(window.Handle, handles)).ToArray();
    }

    /// <summary>Visible top-level windows whose owner chain leads to <paramref name="owner"/>.</summary>
    internal static IReadOnlyList<nint> GetOwnedWindows(nint owner)
    {
        var owned = new List<nint>();
        NativeMethods.EnumWindows((window, _) =>
        {
            if (window != owner && NativeMethods.IsWindowVisible(window) && IsOwnedBy(window, owner))
            {
                owned.Add(window);
            }

            return true;
        }, 0);
        return owned;
    }

    /// <summary>Walks visible owners up to the window that represents the application.</summary>
    internal static nint GetRootOwner(nint window)
    {
        var root = NativeMethods.GetAncestor(window, NativeMethods.GaRoot);
        if (root == 0)
        {
            root = window;
        }

        for (var depth = 0; depth < 16; depth++)
        {
            var owner = NativeMethods.GetWindow(root, NativeMethods.GwOwner);
            if (owner == 0 || !NativeMethods.IsWindowVisible(owner))
            {
                break;
            }

            root = owner;
        }

        return root;
    }

    internal static bool IsDesktop(nint window) => DesktopClasses.Contains(GetClassName(window));

    /// <summary>
    /// Brings a window to the foreground. Windows only grants that to the process
    /// that owns the foreground or handled the last input, and a parked window is
    /// neither, so the two documented workarounds are tried before giving up: a
    /// dummy key event, then borrowing the foreground thread's input state.
    /// </summary>
    public static bool Activate(nint window)
    {
        if (!NativeMethods.IsWindow(window))
        {
            return false;
        }

        if (NativeMethods.IsIconic(window))
        {
            NativeMethods.ShowWindowAsync(window, NativeMethods.SwRestore);
        }

        if (NativeMethods.SetForegroundWindow(window))
        {
            return true;
        }

        NativeMethods.keybd_event(0, 0, 0, 0);
        if (NativeMethods.SetForegroundWindow(window))
        {
            return true;
        }

        var foreground = NativeMethods.GetForegroundWindow();
        var foregroundThread = foreground == 0 ? 0 : NativeMethods.GetWindowThreadProcessId(foreground, out _);
        var ownThread = NativeMethods.GetCurrentThreadId();
        if (foregroundThread == 0 ||
            foregroundThread == ownThread ||
            !NativeMethods.AttachThreadInput(ownThread, foregroundThread, true))
        {
            return false;
        }

        try
        {
            return NativeMethods.SetForegroundWindow(window);
        }
        finally
        {
            NativeMethods.AttachThreadInput(ownThread, foregroundThread, false);
        }
    }

    /// <summary>
    /// Restores a minimized window without the system zoom animation, so the
    /// Stage Manager flight is the only motion on screen.
    /// </summary>
    internal static void RestoreWithoutAnimation(nint window)
    {
        SetTransitionsDisabled(window, true);
        NativeMethods.ShowWindowAsync(window, NativeMethods.SwShowNoActivate);
    }

    /// <summary>
    /// Whether the window's frame is drawn active, which is also what makes DWM
    /// give it the darker shadow of an active window. Windows that draw their own
    /// frame, such as Chromium and Electron apps, never mark it active and keep
    /// the inactive shadow even in the foreground.
    /// </summary>
    internal static bool HasActiveFrame(nint window)
    {
        var info = new NativeMethods.WindowInfo { Size = (uint)Marshal.SizeOf<NativeMethods.WindowInfo>() };
        return NativeMethods.GetWindowInfo(window, ref info) &&
               (info.WindowStatus & NativeMethods.WsActiveCaption) != 0;
    }

    internal static void SetTransitionsDisabled(nint window, bool disabled)
    {
        var value = disabled ? 1 : 0;
        NativeMethods.DwmSetWindowAttribute(window, NativeMethods.DwmwaTransitionsForceDisabled, ref value, sizeof(int));
    }

    private WindowSnapshot? TryCreateSnapshot(nint window, nint foreground, HashSet<uint> processes)
    {
        if (!NativeMethods.IsWindowVisible(window) || NativeMethods.GetAncestor(window, NativeMethods.GaRoot) != window)
        {
            return null;
        }

        NativeMethods.GetWindowThreadProcessId(window, out var processId);
        if (processId == 0 || processId == _currentProcessId)
        {
            return null;
        }

        var extendedStyle = NativeMethods.GetWindowLongPtr(window, NativeMethods.GwlExStyle).ToInt64();
        var isToolWindow = (extendedStyle & NativeMethods.WsExToolWindow) != 0;
        var isAppWindow = (extendedStyle & NativeMethods.WsExAppWindow) != 0;
        // Some desktop apps expose their main surface as an owned window. Only
        // discard genuine tool windows; WS_EX_APPWINDOW explicitly opts in.
        if (isToolWindow && !isAppWindow)
        {
            return null;
        }

        // Always-on-top utilities and non-activating overlays float above the
        // stage; they are not part of it.
        if ((extendedStyle & NativeMethods.WsExTopmost) != 0 ||
            ((extendedStyle & NativeMethods.WsExNoActivate) != 0 && !isAppWindow))
        {
            return null;
        }

        if (NativeMethods.DwmGetWindowAttribute(window, NativeMethods.DwmwaCloaked, out var cloaked, sizeof(int)) == 0 && cloaked != 0)
        {
            return null;
        }

        var className = GetClassName(window);
        if (ExcludedClasses.Contains(className))
        {
            return null;
        }

        var metrics = WindowGeometry.Measure(window);
        if (metrics.FrameBounds.Width < 40 || metrics.FrameBounds.Height < 30)
        {
            return null;
        }

        var title = GetWindowTitle(window);
        if (string.IsNullOrWhiteSpace(title))
        {
            return null;
        }

        var application = ResolveApplication(window, className, processId);
        processes.Add(application.ProcessId);
#if DEBUG
        if (AppFilter is not null && !AppFilter.Contains(Path.GetFileNameWithoutExtension(application.ExecutablePath) ?? string.Empty))
        {
            return null;
        }

#endif
        return new WindowSnapshot(
            window,
            title.Trim(),
            application.Key,
            application.ExecutablePath,
            metrics.Bounds,
            metrics.FrameBounds,
            metrics.Crop,
            NativeMethods.IsIconic(window),
            window == foreground);
    }

    private AppIdentity ResolveApplication(nint window, string className, uint processId)
    {
        // UWP windows are hosted by ApplicationFrameHost; the app itself owns
        // the CoreWindow child. Suspended apps detach it, so they stay apart.
        if (className == ApplicationFrameClass)
        {
            var hostedProcessId = FindHostedProcess(window, processId);
            if (hostedProcessId == 0)
            {
                return new AppIdentity($"frame:{window}", null, processId);
            }

            processId = hostedProcessId;
        }

        if (_applications.TryGetValue(processId, out var cached))
        {
            return cached;
        }

        var path = TryGetProcessPath(processId);
        var identity = new AppIdentity(path?.ToUpperInvariant() ?? $"pid:{processId}", path, processId);
        _applications[processId] = identity;
        return identity;
    }

    private static uint FindHostedProcess(nint frame, uint frameProcessId)
    {
        uint hostedProcessId = 0;
        NativeMethods.EnumChildWindows(frame, (child, _) =>
        {
            if (GetClassName(child) != CoreWindowClass)
            {
                return true;
            }

            NativeMethods.GetWindowThreadProcessId(child, out var childProcessId);
            if (childProcessId != 0 && childProcessId != frameProcessId)
            {
                hostedProcessId = childProcessId;
                return false;
            }

            return true;
        }, 0);
        return hostedProcessId;
    }

    private static string? TryGetProcessPath(uint processId)
    {
        var process = NativeMethods.OpenProcess(NativeMethods.ProcessQueryLimitedInformation, false, processId);
        if (process == 0)
        {
            return null;
        }

        try
        {
            var buffer = new StringBuilder(1024);
            var size = buffer.Capacity;
            return NativeMethods.QueryFullProcessImageName(process, 0, buffer, ref size)
                ? buffer.ToString(0, size)
                : null;
        }
        finally
        {
            NativeMethods.CloseHandle(process);
        }
    }

    private static bool HasListedOwner(nint window, IReadOnlySet<nint> listed)
    {
        var owner = NativeMethods.GetWindow(window, NativeMethods.GwOwner);
        for (var depth = 0; owner != 0 && depth < 16; depth++)
        {
            if (listed.Contains(owner))
            {
                return true;
            }

            owner = NativeMethods.GetWindow(owner, NativeMethods.GwOwner);
        }

        return false;
    }

    private static bool IsOwnedBy(nint window, nint root)
    {
        var owner = NativeMethods.GetWindow(window, NativeMethods.GwOwner);
        for (var depth = 0; owner != 0 && depth < 16; depth++)
        {
            if (owner == root)
            {
                return true;
            }

            owner = NativeMethods.GetWindow(owner, NativeMethods.GwOwner);
        }

        return false;
    }

    private static string GetWindowTitle(nint window)
    {
        var length = NativeMethods.GetWindowTextLength(window);
        if (length <= 0)
        {
            return string.Empty;
        }

        var buffer = new StringBuilder(length + 1);
        NativeMethods.GetWindowText(window, buffer, buffer.Capacity);
        return buffer.ToString();
    }

    private static string GetClassName(nint window)
    {
        var buffer = new StringBuilder(256);
        NativeMethods.GetClassName(window, buffer, buffer.Capacity);
        return buffer.ToString();
    }

    private readonly record struct AppIdentity(string Key, string? ExecutablePath, uint ProcessId);
}
