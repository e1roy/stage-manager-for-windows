using System.Drawing;
using StageManager.Services;

namespace StageManager.Models;

internal sealed record WindowSnapshot(
    nint Handle,
    string Title,
    string AppKey,
    string? ExecutablePath,
    Rectangle Bounds,
    Rectangle FrameBounds,
    ThumbnailCrop Crop,
    bool IsMinimized,
    bool IsForeground)
{
    internal double AspectRatio => FrameBounds.Width > 0 && FrameBounds.Height > 0
        ? (double)FrameBounds.Width / FrameBounds.Height
        : 16d / 10d;
}
