using System.Windows;

namespace StageManager;

public partial class App : System.Windows.Application
{
    private MainWindow? _mainWindow;

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        // Parked windows live off-screen; put them back if StageBar goes away
        // unexpectedly or the session ends, so no window is ever stranded.
        DispatcherUnhandledException += (_, _) => _mainWindow?.RestoreParkedWindows();
        AppDomain.CurrentDomain.UnhandledException += (_, _) => _mainWindow?.RestoreParkedWindows();
        SessionEnding += (_, _) => _mainWindow?.RestoreParkedWindows();
        _mainWindow = new MainWindow();
        MainWindow = _mainWindow;
        _mainWindow.Show();
        if (e.Args.Any(static argument => argument.Equals("--settings", StringComparison.OrdinalIgnoreCase)))
        {
            Dispatcher.BeginInvoke(_mainWindow.OpenSettings);
        }
    }
}
