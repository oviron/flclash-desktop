using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Threading;
using Microsoft.Win32;

namespace FlClash;

/// The window process. FlClash.exe starts it with the pipe to talk on and,
/// at times, the page to show; it lives while the window is open.
public static class App
{
    static Mutex? single;

    public static bool Debug = Environment.GetCommandLineArgs().Contains("--debug");

    /// Where the window keeps its settings and log: the folder FlClash.exe
    /// names, which is beside the program in a portable copy.
    public static readonly string DataDir = DataFolder();

    /// --theme light|dark draws the window in a theme other than the system's, to look at it.
    public static readonly string? Theme = Arg("--theme");

    static string? Arg(string name)
    {
        var argv = Environment.GetCommandLineArgs();
        int i = Array.IndexOf(argv, name);
        return i >= 0 && i + 1 < argv.Length ? argv[i + 1] : null;
    }

    static string DataFolder()
    {
        var dir = Arg("--data");
        if (!string.IsNullOrEmpty(dir)) return dir!;
        return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "flclash-desktop");
    }

    public static void Note(Exception e)
    {
        try
        {
            Directory.CreateDirectory(DataDir);
            File.AppendAllText(Path.Combine(DataDir, "window.log"), DateTime.Now.ToString("s") + " " + e + "\n");
        }
        catch (Exception) { }
    }

    [STAThread]
    static int Main()
    {
        // What breaks is written down where it can be found, beside the window's settings.
        AppDomain.CurrentDomain.UnhandledException += (_, e) => { if (e.ExceptionObject is Exception x) Note(x); };
        // --debug writes down every exception, also those that are handled.
        if (Debug) AppDomain.CurrentDomain.FirstChanceException += (_, e) => Note(e.Exception);
        try { return Run(); }
        catch (Exception e) { Note(e); throw; }
    }

    static int Run()
    {
        L.Use(System.Globalization.CultureInfo.CurrentUICulture.TwoLetterISOLanguageName);
        var pipe = Arg("--pipe") ?? Agent.DefaultPipe();
        var page = Arg("--page");

        var agent = Agent.Connect(pipe, 3000, out bool foreign);
        if (agent == null)
        {
            MessageBox(IntPtr.Zero, foreign
                ? L.T("Another program answered in place of FlClash. Nothing was sent to it.")
                : L.T("FlClash is not running. Start FlClash, then open its window from the icon in the notification area."),
                "FlClash", 0x10);
            return 1;
        }
        // One window: a second start asks the agent to bring the first one up.
        single = new Mutex(true, @"Local\FlClash.Window", out bool first);
        if (!first)
        {
            agent.Open(page);
            agent.Finish(2000);
            return 0;
        }

        var app = new Application { ShutdownMode = ShutdownMode.OnMainWindowClose };
        app.DispatcherUnhandledException += (_, e) => Note(e.Exception);
        app.Resources = Ui.Styles();
        if (Theme != null) Ui.Dark = Theme != "light";
        Ui.ApplyTheme();

        var window = new MainWindow(agent, page);
        // The system's theme and accent, as they change.
        SystemEvents.UserPreferenceChanged += (_, e) =>
        {
            if (e.Category is UserPreferenceCategory.General or UserPreferenceCategory.Color or UserPreferenceCategory.VisualStyle)
                app.Dispatcher.BeginInvoke(new Action(window.Themed), DispatcherPriority.Background);
        };
        agent.Start(app.Dispatcher);
        agent.Hello();
        agent.Watch(true);
        window.Show();
        window.Front();
        return app.Run(window);
    }

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    static extern int MessageBox(IntPtr hwnd, string text, string caption, uint type);
}
