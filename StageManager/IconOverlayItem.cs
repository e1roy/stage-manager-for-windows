using System.Windows;
using System.Windows.Media;

namespace StageManager;

internal readonly record struct IconOverlayItem(
    nint Handle,
    ImageSource Icon,
    Rect Bounds,
    double Opacity);
