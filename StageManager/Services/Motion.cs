namespace StageManager.Services;

/// <summary>
/// A damped spring. Changing <see cref="Target"/> mid-flight keeps the current
/// velocity, so interrupted animations continue smoothly instead of restarting.
/// </summary>
internal sealed class Spring
{
    private const double MaximumStep = 1d / 240d;
    private readonly double _stiffness;
    private readonly double _damping;
    private readonly double _precision;

    internal Spring(double value, double response, double dampingRatio = 1, double precision = 0.001)
    {
        Value = value;
        Target = value;
        var angularFrequency = 2 * Math.PI / response;
        _stiffness = angularFrequency * angularFrequency;
        _damping = 2 * dampingRatio * angularFrequency;
        _precision = precision;
    }

    internal double Value { get; private set; }

    internal double Target { get; set; }

    internal double Velocity { get; private set; }

    internal bool IsSettled => Value == Target && Velocity == 0;

    internal void Snap(double value)
    {
        Value = value;
        Target = value;
        Velocity = 0;
    }

    /// <summary>Advances the spring and returns <c>true</c> while it is still moving.</summary>
    internal bool Step(double seconds)
    {
        if (IsSettled)
        {
            return false;
        }

        while (seconds > 0)
        {
            var step = Math.Min(seconds, MaximumStep);
            var acceleration = -_stiffness * (Value - Target) - _damping * Velocity;
            Velocity += acceleration * step;
            Value += Velocity * step;
            seconds -= step;
        }

        if (Math.Abs(Value - Target) < _precision && Math.Abs(Velocity) < _precision * 8)
        {
            Value = Target;
            Velocity = 0;
            return false;
        }

        return true;
    }
}

internal static class Easing
{
    private const double Stiffness = 7.5;
    private const double LaunchVelocity = 0.6;
    private static readonly double EndValue = Spring(Stiffness);

    /// <summary>
    /// The position curve of a critically damped spring launched toward its
    /// target, normalised to land exactly on 1. It leaves briskly, travels most
    /// of the distance early and settles without overshoot, like macOS window
    /// motion.
    /// </summary>
    internal static double CriticallyDamped(double progress) =>
        Spring(Stiffness * Math.Clamp(progress, 0, 1)) / EndValue;

    // A launch velocity below the critical one keeps the curve overshoot-free.
    private static double Spring(double x) => 1 - (1 + (1 - LaunchVelocity) * x) * Math.Exp(-x);
}
