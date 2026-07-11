namespace StageManager.Models;

public sealed record WindowSnapshot(
    nint Handle,
    string Title,
    string ProcessName,
    bool IsMinimized,
    bool IsForeground);
