namespace StageManager.Models;

/// <summary>
/// The windows of one application, front-most first. Stage Manager moves an
/// application on and off the stage as a unit.
/// </summary>
internal sealed record AppGroup(string Key, IReadOnlyList<WindowSnapshot> Windows)
{
    internal WindowSnapshot Front => Windows[0];
}
