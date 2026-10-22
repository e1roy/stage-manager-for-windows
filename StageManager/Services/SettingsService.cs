using System.Text.Json;
using System.IO;
using StageManager.Models;

namespace StageManager.Services;

internal sealed class SettingsService
{
    private static readonly JsonSerializerOptions SerializerOptions = new() { WriteIndented = true };
    private readonly string _settingsPath = Path.Combine(AppPaths.DataDirectory, "settings.json");

    internal StageManagerSettings Load()
    {
        try
        {
            if (!File.Exists(_settingsPath))
            {
                return StageManagerSettings.Default;
            }

            var settings = JsonSerializer.Deserialize<StageManagerSettings>(
                File.ReadAllText(_settingsPath),
                SerializerOptions);
            return Normalize(settings ?? StageManagerSettings.Default);
        }
        catch (JsonException)
        {
            return StageManagerSettings.Default;
        }
        catch (IOException)
        {
            return StageManagerSettings.Default;
        }
    }

    internal void Save(StageManagerSettings settings)
    {
        var directory = Path.GetDirectoryName(_settingsPath)!;
        Directory.CreateDirectory(directory);
        File.WriteAllText(
            _settingsPath,
            JsonSerializer.Serialize(Normalize(settings), SerializerOptions));
    }

    internal static StageManagerSettings Normalize(StageManagerSettings settings) => settings with
    {
        DisplayWidth = Math.Clamp(
            settings.DisplayWidth,
            LayoutMetrics.MinimumWidth,
            LayoutMetrics.MaximumWidth),
        IconScale = Math.Clamp(
            settings.IconScale,
            LayoutMetrics.MinimumIconScale,
            LayoutMetrics.MaximumIconScale)
    };
}
