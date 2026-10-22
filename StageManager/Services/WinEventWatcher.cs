using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// Listens to system window events so the stage reacts to Alt+Tab, taskbar
/// clicks and newly opened windows immediately instead of on a polling tick.
/// Out-of-context hooks are delivered on the thread that installed them.
/// </summary>
internal sealed class WinEventWatcher : IDisposable
{
    private readonly NativeMethods.WinEventProc _callback;
    private readonly List<nint> _hooks = [];

    internal WinEventWatcher()
    {
        _callback = OnWinEvent;
        Hook(NativeMethods.EventSystemForeground, NativeMethods.EventSystemForeground);
        Hook(NativeMethods.EventSystemMoveSizeEnd, NativeMethods.EventSystemMoveSizeEnd);
        Hook(NativeMethods.EventSystemMinimizeStart, NativeMethods.EventSystemMinimizeEnd);
        Hook(NativeMethods.EventObjectDestroy, NativeMethods.EventObjectHide);
        Hook(NativeMethods.EventObjectLocationChange, NativeMethods.EventObjectNameChange);
        Hook(NativeMethods.EventObjectCloaked, NativeMethods.EventObjectUncloaked);
    }

    internal event Action<nint>? ForegroundChanged;

    internal event Action<nint, uint>? WindowChanged;

    public void Dispose()
    {
        foreach (var hook in _hooks)
        {
            NativeMethods.UnhookWinEvent(hook);
        }

        _hooks.Clear();
    }

    private void Hook(uint first, uint last)
    {
        var hook = NativeMethods.SetWinEventHook(
            first,
            last,
            0,
            _callback,
            0,
            0,
            NativeMethods.WinEventOutOfContext | NativeMethods.WinEventSkipOwnProcess);
        if (hook != 0)
        {
            _hooks.Add(hook);
        }
    }

    private void OnWinEvent(nint hook, uint eventType, nint window, int objectId, int childId, uint eventThread, uint eventTime)
    {
        if (window == 0 || objectId != NativeMethods.ObjIdWindow || childId != 0)
        {
            return;
        }

        if (eventType == NativeMethods.EventSystemForeground)
        {
            ForegroundChanged?.Invoke(window);
            return;
        }

        // Destroyed windows can no longer be queried, so let them through.
        if (eventType != NativeMethods.EventObjectDestroy &&
            NativeMethods.GetAncestor(window, NativeMethods.GaRoot) != window)
        {
            return;
        }

        WindowChanged?.Invoke(window, eventType);
    }
}
