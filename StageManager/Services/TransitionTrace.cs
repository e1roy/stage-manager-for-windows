using System.IO;

namespace StageManager.Services;

/// <summary>
/// Appends what the hand-over actually did to a log, so a switch that looks wrong
/// in a recording can be compared against the rectangles the flights used. The
/// file is trimmed instead of rotating: only the latest switches matter.
/// </summary>
internal static class TransitionTrace
{
    private const long MaximumBytes = 512 * 1024;
    private static readonly object Gate = new();
    private static readonly string LogPath = Path.Combine(AppPaths.DataDirectory, "transition.log");

    internal static void Line(string message)
    {
        lock (Gate)
        {
            try
            {
                Directory.CreateDirectory(AppPaths.DataDirectory);
                var file = new FileInfo(LogPath);
                if (file.Exists && file.Length > MaximumBytes)
                {
                    file.Delete();
                }

                File.AppendAllText(
                    LogPath,
                    $"{DateTime.Now:HH:mm:ss.fff} {message}{Environment.NewLine}");
            }
            catch (Exception exception) when (exception is IOException or UnauthorizedAccessException)
            {
                // Diagnostics must never break a switch.
            }
        }
    }
}
