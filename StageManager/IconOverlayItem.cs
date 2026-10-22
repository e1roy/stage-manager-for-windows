using System.Windows;
using System.Windows.Media;

namespace StageManager;

internal readonly record struct IconOverlayItem(
    string Key,
    ImageSource Icon,
    Rect Bounds,
    double Opacity);
