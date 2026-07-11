using System.Windows;

namespace StageManager;

public partial class App : System.Windows.Application
{
    private MainWindow? _mainWindow;

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        _mainWindow = new MainWindow();
        MainWindow = _mainWindow;
        _mainWindow.Show();
        if (e.Args.Any(static argument => argument.Equals("--settings", StringComparison.OrdinalIgnoreCase)))
        {
            Dispatcher.BeginInvoke(_mainWindow.OpenSettings);
        }
    }
}
