using System.Drawing;
using System.IO;
using System.Text.Json;
using System.Windows.Forms;
using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// Hides inactive windows by moving them just outside the desktop. Unlike
/// minimizing, the window keeps its size and state, so it returns instantly
/// without relayout or repaint. Every move is journaled so windows can be
/// recovered after an unexpected exit.
/// </summary>
internal sealed class WindowParkingService
{
    private const int ParkingGap = 160;
    // No redraw: Windows does not paint off-screen windows, so moving one back
    // would make the app erase and repaint everything. Apps that repaint
    // asynchronously (Java IDEs, for example) then show a blank window for a
    // moment. Without the redraw the window keeps showing its intact image.
    private const uint MoveFlags = NativeMethods.SwpNoRedraw |
                                   NativeMethods.SwpNoSize |
                                   NativeMethods.SwpNoZOrder |
                                   NativeMethods.SwpNoActivate |
                                   NativeMethods.SwpNoOwnerZOrder |
                                   NativeMethods.SwpAsyncWindowPos;
    private static readonly TimeSpan SettleTime = TimeSpan.FromSeconds(1);

    private readonly Dictionary<nint, ParkedWindow> _parked = [];
    private readonly string _journalPath = Path.Combine(AppPaths.DataDirectory, "parked-windows.json");

    internal bool IsParked(nint window) => _parked.ContainsKey(window);

    internal bool TryGetHome(nint window, out Rectangle home)
    {
        if (_parked.TryGetValue(window, out var parked))
        {
            home = parked.Home;
            return true;
        }

        home = Rectangle.Empty;
        return false;
    }

    internal void Park(nint window)
    {
        if (_parked.ContainsKey(window) || !TryParkSingle(window, 0))
        {
            return;
        }

        // Dialogs and palettes owned by the window leave the stage with it.
        foreach (var owned in WindowCatalog.GetOwnedWindows(window))
        {
            if (!_parked.ContainsKey(owned))
            {
                TryParkSingle(owned, window);
            }
        }

        SaveJournal();
    }

    /// <summary>
    /// Returns a window and its parked dialogs home, raised above other windows
    /// even when Windows does not allow StageBar to move keyboard focus.
    /// </summary>
    internal void Unpark(nint window)
    {
        if (!_parked.TryGetValue(window, out var root))
        {
            return;
        }

        foreach (var (handle, parked) in _parked.Where(entry => entry.Key == window || entry.Value.Owner == window).ToArray())
        {
            _parked.Remove(handle);
            MoveTo(handle, parked.Home.Location, bringToFront: true);
        }

        // A dialog opened while its owner was parked was placed relative to the
        // parked position; bring it along by the same offset.
        var offsetX = root.Home.X - root.Spot.X;
        var offsetY = root.Home.Y - root.Spot.Y;
        foreach (var owned in WindowCatalog.GetOwnedWindows(window))
        {
            var bounds = WindowGeometry.GetBounds(owned);
            if (!IsOnAnyScreen(bounds))
            {
                MoveTo(owned, new Point(bounds.X + offsetX, bounds.Y + offsetY));
            }
        }

        SaveJournal();
    }

    internal void RestoreAll()
    {
        // A window a flight was priming is invisible and click-through; handing it
        // back in that state would leave the user without it.
        WindowPriming.RevealAll();
        if (_parked.Count == 0)
        {
            return;
        }

        foreach (var (handle, parked) in _parked)
        {
            if (NativeMethods.IsWindow(handle))
            {
                MoveTo(handle, parked.Home.Location);
                RefreshContent(handle);
            }
        }

        _parked.Clear();
        SaveJournal();
    }

    /// <summary>
    /// Forgets windows that closed and returns parked application windows that
    /// something else moved back onto the desktop, for example when Windows
    /// re-maximizes windows after the work area changes.
    /// </summary>
    internal IReadOnlyList<nint> CollectEscaped()
    {
        var escaped = new List<nint>();
        var changed = false;
        foreach (var (handle, parked) in _parked.ToArray())
        {
            if (!NativeMethods.IsWindow(handle))
            {
                _parked.Remove(handle);
                changed = true;
                continue;
            }

            if (DateTime.UtcNow - parked.ParkedAt < SettleTime)
            {
                continue;
            }

            var bounds = WindowGeometry.GetBounds(handle);
            if (bounds.Location == parked.Spot || NativeMethods.IsIconic(handle))
            {
                continue;
            }

            _parked.Remove(handle);
            changed = true;
            if (parked.Owner == 0 && IsOnAnyScreen(bounds))
            {
                escaped.Add(handle);
            }
        }

        if (changed)
        {
            SaveJournal();
        }

        return escaped;
    }

    /// <summary>Returns windows left parked by a previous session that ended unexpectedly.</summary>
    internal void RecoverFromJournal()
    {
        try
        {
            if (!File.Exists(_journalPath))
            {
                return;
            }

            var entries = JsonSerializer.Deserialize<JournalEntry[]>(File.ReadAllText(_journalPath)) ?? [];
            foreach (var entry in entries)
            {
                var handle = (nint)entry.Handle;
                if (NativeMethods.IsWindow(handle) &&
                    WindowGeometry.GetBounds(handle).Location == new Point(entry.SpotX, entry.SpotY))
                {
                    // The crash may have happened while the window was hidden for a
                    // flight, leaving it invisible and click-through off the desktop.
                    WindowPriming.Reveal(handle);
                    var home = ClampOnScreen(new Point(entry.HomeX, entry.HomeY), WindowGeometry.GetBounds(handle));
                    MoveTo(handle, home);
                    RefreshContent(handle);
                }
            }

            File.Delete(_journalPath);
        }
        catch (Exception exception) when (exception is IOException or JsonException or UnauthorizedAccessException)
        {
            // Recovery is best effort; a damaged journal must not block startup.
        }
    }

    /// <summary>
    /// Keeps a recovered window reachable when the journal recorded a home that is
    /// itself off the desktop, which earlier builds could do.
    /// </summary>
    private static Point ClampOnScreen(Point home, Rectangle bounds)
    {
        if (IsOnAnyScreen(new Rectangle(home, bounds.Size)))
        {
            return home;
        }

        var screen = Screen.PrimaryScreen?.WorkingArea ?? SystemInformation.VirtualScreen;
        return new Point(
            screen.Left + Math.Max(0, (screen.Width - bounds.Width) / 2),
            screen.Top + Math.Max(0, (screen.Height - bounds.Height) / 2));
    }

    private bool TryParkSingle(nint window, nint owner)
    {
        if (!NativeMethods.IsWindow(window) || NativeMethods.IsIconic(window))
        {
            return false;
        }

        var home = WindowGeometry.GetBounds(window);
        if (home.IsEmpty)
        {
            return false;
        }

        if (!IsOnAnyScreen(home))
        {
            // Parking a window that is already off the desktop would record the
            // parking spot as its home, and the window could never come back.
            return false;
        }

        var spot = FindParkingSpot(home);
        if (!MoveTo(window, spot))
        {
            return false;
        }

        _parked[window] = new ParkedWindow(home, spot, owner, DateTime.UtcNow);
        return true;
    }

    /// <summary>
    /// Picks an off-desktop position whose nearest monitor is still the
    /// window's own monitor, so per-monitor DPI-aware apps never rescale.
    /// </summary>
    private static Point FindParkingSpot(Rectangle bounds)
    {
        var desktop = SystemInformation.VirtualScreen;
        var home = ToNative(bounds);
        var monitor = NativeMethods.MonitorFromRect(ref home, NativeMethods.MonitorDefaultToNearest);
        Point[] candidates =
        [
            new(desktop.Right + ParkingGap, bounds.Top),
            new(desktop.Left - bounds.Width - ParkingGap, bounds.Top),
            new(bounds.Left, desktop.Bottom + ParkingGap),
            new(bounds.Left, desktop.Top - bounds.Height - ParkingGap)
        ];

        foreach (var candidate in candidates)
        {
            var rect = ToNative(new Rectangle(candidate, bounds.Size));
            if (NativeMethods.MonitorFromRect(ref rect, NativeMethods.MonitorDefaultToNearest) == monitor)
            {
                return candidate;
            }
        }

        return candidates[0];
    }

    /// <summary>
    /// Lets a window that returned home catch up on anything it could not paint
    /// while parked. Nothing is erased first, so its current image stays on
    /// screen until the new one is ready.
    /// </summary>
    internal static void RefreshContent(nint window) =>
        NativeMethods.RedrawWindow(
            window,
            0,
            0,
            NativeMethods.RdwInvalidate | NativeMethods.RdwNoErase | NativeMethods.RdwAllChildren | NativeMethods.RdwFrame);

    private static bool MoveTo(nint window, Point location, bool bringToFront = false) =>
        NativeMethods.SetWindowPos(
            window,
            NativeMethods.HwndTop,
            location.X,
            location.Y,
            0,
            0,
            bringToFront ? MoveFlags & ~NativeMethods.SwpNoZOrder : MoveFlags);

    private static bool IsOnAnyScreen(Rectangle bounds) =>
        Screen.AllScreens.Any(screen => screen.Bounds.IntersectsWith(bounds));

    private static NativeMethods.Rect ToNative(Rectangle bounds) => new()
    {
        Left = bounds.Left,
        Top = bounds.Top,
        Right = bounds.Right,
        Bottom = bounds.Bottom
    };

    private void SaveJournal()
    {
        try
        {
            if (_parked.Count == 0)
            {
                File.Delete(_journalPath);
                return;
            }

            Directory.CreateDirectory(Path.GetDirectoryName(_journalPath)!);
            var entries = _parked.Select(static entry => new JournalEntry(
                entry.Key,
                entry.Value.Home.X,
                entry.Value.Home.Y,
                entry.Value.Spot.X,
                entry.Value.Spot.Y));
            File.WriteAllText(_journalPath, JsonSerializer.Serialize(entries));
        }
        catch (Exception exception) when (exception is IOException or UnauthorizedAccessException)
        {
            // Parking still works without the journal; only crash recovery is lost.
        }
    }

    private sealed record ParkedWindow(Rectangle Home, Point Spot, nint Owner, DateTime ParkedAt);

    private sealed record JournalEntry(long Handle, int HomeX, int HomeY, int SpotX, int SpotY);
}
