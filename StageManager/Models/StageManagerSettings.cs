namespace StageManager.Models;

public enum DisplayMode
{
    Fixed,
    Floating
}

public sealed record StageManagerSettings(
    DisplayMode DisplayMode,
    double DisplayWidth,
    int CardCount)
{
    public static StageManagerSettings Default { get; } = new(DisplayMode.Fixed, 286, 3);
}
