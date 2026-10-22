using System.IO;

namespace StageManager.Services;

internal static class AppPaths
{
    /// <summary>
    /// Settings and the parking journal live here. The legacy directory name is
    /// intentionally retained so upgrades preserve settings from earlier builds.
    /// </summary>
    internal static string DataDirectory { get; } =
#if DEBUG
        // Development aid: lets a test run use its own settings and journal.
        Environment.GetEnvironmentVariable("STAGEBAR_DATA_DIR") is { Length: > 0 } dataDirectory
            ? dataDirectory
            :
#endif
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "StageManager");
}
