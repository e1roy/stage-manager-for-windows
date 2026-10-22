using System.Windows;
using System.Windows.Interop;
using System.Windows.Media;
using StageManager.Interop;
using StageManager.Models;
using Windows.UI.ViewManagement;
using Brush = System.Windows.Media.Brush;
using Brushes = System.Windows.Media.Brushes;
using Color = System.Windows.Media.Color;

namespace StageManager;

/// <summary>
/// Settings laid out like Windows 11 Settings: the system title bar over a
/// Mica backdrop, following the system's light/dark mode and accent color.
/// </summary>
public partial class SettingsWindow : Window
{
    // Mica as a system backdrop needs Windows 11 22H2; earlier systems get
    // the solid background Settings itself falls back to.
    private static readonly bool SupportsMica = Environment.OSVersion.Version.Build >= 22621;

    // Windows' Fluent color tokens as (dark, light) ARGB.
    private static readonly (string Key, uint Dark, uint Light)[] Palette =
    [
        ("TextPrimary", 0xFFFFFFFF, 0xE4000000),
        ("TextSecondary", 0xC5FFFFFF, 0x9E000000),
        ("TextOnAccent", 0xFF000000, 0xFFFFFFFF),
        ("CardBackground", 0x0DFFFFFF, 0xB3FFFFFF),
        ("CardStroke", 0x19000000, 0x0F000000),
        ("Divider", 0x15FFFFFF, 0x0F000000),
        ("ControlStrong", 0x8BFFFFFF, 0x72000000),
        ("ControlAltFill", 0x19000000, 0x06000000),
        ("ControlAltFillHover", 0x0BFFFFFF, 0x0F000000),
        ("ControlSolidFill", 0xFF454545, 0xFFFFFFFF),
        ("ThumbStroke", 0x23FFFFFF, 0x29000000),
        ("WindowBackground", 0xFF202020, 0xFFF3F3F3)
    ];

    private readonly UISettings _uiSettings = new();
    private nint _handle;
    private bool _isDark;
    private bool _isReady;

    public SettingsWindow(StageManagerSettings settings)
    {
        InitializeComponent();
        ApplyTheme();
        _uiSettings.ColorValuesChanged += UiSettings_ColorValuesChanged;

        FixedModeRadio.IsChecked = settings.DisplayMode is DisplayMode.Fixed;
        FloatingModeRadio.IsChecked = settings.DisplayMode is DisplayMode.Floating;
        HideWindowsToggle.IsChecked = settings.HideInactiveWindows;
        WidthSlider.Value = settings.DisplayWidth;
        IconSizeSlider.Value = settings.IconScale * 100;
        UpdateLabels();
        _isReady = true;
    }

    public event Action<StageManagerSettings>? SettingsChanged;

    protected override void OnSourceInitialized(EventArgs e)
    {
        base.OnSourceInitialized(e);
        _handle = new WindowInteropHelper(this).Handle;
        if (SupportsMica)
        {
            // Let DWM's backdrop show through the whole client area.
            if (HwndSource.FromHwnd(_handle) is { CompositionTarget: { } target })
            {
                target.BackgroundColor = Colors.Transparent;
            }

            var margins = new NativeMethods.Margins { Left = -1, Right = -1, Top = -1, Bottom = -1 };
            NativeMethods.DwmExtendFrameIntoClientArea(_handle, ref margins);
            var backdrop = NativeMethods.DwmsbtMainWindow;
            NativeMethods.DwmSetWindowAttribute(_handle, NativeMethods.DwmwaSystemBackdropType, ref backdrop, sizeof(int));
        }

        ApplyFrameTheme();
    }

    protected override void OnClosed(EventArgs e)
    {
        _uiSettings.ColorValuesChanged -= UiSettings_ColorValuesChanged;
        base.OnClosed(e);
    }

    // Raised on a background thread when the mode or accent color changes.
    private void UiSettings_ColorValuesChanged(UISettings sender, object args) =>
        Dispatcher.BeginInvoke(ApplyTheme);

    private void ApplyTheme()
    {
        // Settings reads dark mode the same way: a dark app background.
        var background = _uiSettings.GetColorValue(UIColorType.Background);
        _isDark = background.R < 128;
        foreach (var (key, dark, light) in Palette)
        {
            Resources[key] = CreateBrush(_isDark ? dark : light);
        }

        // Fluent fills accent controls with a lighter accent in dark mode.
        var accent = _uiSettings.GetColorValue(_isDark ? UIColorType.AccentLight2 : UIColorType.AccentDark1);
        Resources["Accent"] = CreateBrush(Color.FromRgb(accent.R, accent.G, accent.B));
        Resources["AccentHover"] = CreateBrush(Color.FromArgb(0xE6, accent.R, accent.G, accent.B));
        Background = SupportsMica ? Brushes.Transparent : (Brush)Resources["WindowBackground"];
        ApplyFrameTheme();
    }

    /// <summary>Gives the title bar and Mica the current mode.</summary>
    private void ApplyFrameTheme()
    {
        if (_handle == 0)
        {
            return;
        }

        var useDarkMode = _isDark ? 1 : 0;
        NativeMethods.DwmSetWindowAttribute(_handle, NativeMethods.DwmwaUseImmersiveDarkMode, ref useDarkMode, sizeof(int));
    }

    private static SolidColorBrush CreateBrush(uint argb) => CreateBrush(Color.FromArgb(
        (byte)(argb >> 24),
        (byte)(argb >> 16),
        (byte)(argb >> 8),
        (byte)argb));

    private static SolidColorBrush CreateBrush(Color color)
    {
        var brush = new SolidColorBrush(color);
        brush.Freeze();
        return brush;
    }

    private void ModeRadio_Checked(object sender, RoutedEventArgs e) => PublishChanges();

    private void HideWindowsToggle_Changed(object sender, RoutedEventArgs e) => PublishChanges();

    private void Slider_ValueChanged(object sender, RoutedPropertyChangedEventArgs<double> e)
    {
        if (!IsInitialized)
        {
            return;
        }

        UpdateLabels();
        PublishChanges();
    }

    private void UpdateLabels()
    {
        WidthValueText.Text = $"{WidthSlider.Value:0} px";
        IconSizeValueText.Text = $"{IconSizeSlider.Value:0}%";
    }

    private void PublishChanges()
    {
        if (!_isReady)
        {
            return;
        }

        SettingsChanged?.Invoke(new StageManagerSettings(
            FixedModeRadio.IsChecked == true ? DisplayMode.Fixed : DisplayMode.Floating,
            WidthSlider.Value,
            HideWindowsToggle.IsChecked == true,
            IconSizeSlider.Value / 100));
    }
}
