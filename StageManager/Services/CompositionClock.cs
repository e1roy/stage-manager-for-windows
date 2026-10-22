using System.Diagnostics;
using System.Windows.Threading;
using StageManager.Interop;

namespace StageManager.Services;

/// <summary>
/// Ticks on the UI thread once per DWM composition, at the display's refresh
/// rate. WPF's Rendering event only keeps that pace while WPF has visuals of its
/// own to redraw: a layer that moves nothing but DWM thumbnails gets about a
/// quarter of it (measured: 32 of 120 frames a second).
/// </summary>
internal sealed class CompositionClock
{
    private readonly Dispatcher _dispatcher;
    private readonly Action _tick;
    private readonly Action _onTick;
    private readonly object _gate = new();
    private Thread? _thread;
    private volatile bool _isRunning;
    private int _isTickPending;
    private int _flushes;
    private int _skipped;
    private long _lastFlushAt;

    internal CompositionClock(Dispatcher dispatcher, Action tick)
    {
        _dispatcher = dispatcher;
        _tick = tick;
        _onTick = OnTick;
    }

    internal void Start()
    {
        lock (_gate)
        {
            _isRunning = true;
            if (_thread is not null)
            {
                return;
            }

            _thread = new Thread(Run)
            {
                IsBackground = true,
                Name = "StageBar composition clock",
                Priority = ThreadPriority.AboveNormal
            };
            _thread.Start();
        }
    }

    internal void Stop() => _isRunning = false;

    /// <summary>
    /// Compositions seen and ticks dropped because the UI thread was still busy
    /// with the previous one, since the last call.
    /// </summary>
    internal (int Flushes, int Skipped) TakeCounts() =>
        (Interlocked.Exchange(ref _flushes, 0), Interlocked.Exchange(ref _skipped, 0));

    /// <summary>How long ago, in milliseconds, the composition that caused the current tick ended.</summary>
    internal double TickLatency =>
        Stopwatch.GetElapsedTime(Interlocked.Read(ref _lastFlushAt)).TotalMilliseconds;

    private void Run()
    {
        while (true)
        {
            lock (_gate)
            {
                if (!_isRunning)
                {
                    _thread = null;
                    return;
                }
            }

            if (NativeMethods.DwmFlush() != 0)
            {
                // Without composition there is no frame to wait for.
                Thread.Sleep(4);
            }

            Interlocked.Increment(ref _flushes);

            // A busy UI thread skips frames instead of queueing them up.
            if (Interlocked.Exchange(ref _isTickPending, 1) == 0)
            {
                Interlocked.Exchange(ref _lastFlushAt, Stopwatch.GetTimestamp());
                _dispatcher.BeginInvoke(DispatcherPriority.Send, _onTick);
            }
            else
            {
                Interlocked.Increment(ref _skipped);
            }
        }
    }

    private void OnTick()
    {
        Volatile.Write(ref _isTickPending, 0);
        if (_isRunning)
        {
            _tick();
        }
    }
}
