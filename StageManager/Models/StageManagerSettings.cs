namespace StageManager.Models;

public enum DisplayMode
{
    Fixed,
    Floating
}

public sealed record StageManagerSettings(
    DisplayMode DisplayMode,
    double DisplayWidth,
    bool HideInactiveWindows = true,
    double IconScale = 1)
{
    public static StageManagerSettings Default { get; } = new(DisplayMode.Fixed, 200);
}
