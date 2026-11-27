using System.Runtime.InteropServices;
using Microsoft.UI.Xaml;

namespace FlClash;

/// The window process. FlClash.exe starts it with the pipe to talk on and,
/// at times, the page to show; it lives while the window is open.
public partial class App : Application
{
    MainWindow? window;
    static Mutex? single;

    public App()
    {
        InitializeComponent();
        // --theme light|dark draws the window in a theme other than the system's, to look at it.
        var argv = Environment.GetCommandLineArgs();
        int at = Array.IndexOf(argv, "--theme");
        if (at >= 0 && at + 1 < argv.Length) RequestedTheme = argv[at + 1] == "light" ? ApplicationTheme.Light : ApplicationTheme.Dark;
        // What breaks is written down where it can be found, beside the window's settings.
        UnhandledException += (_, e) => Note(e.Exception);
        // --debug writes down every exception, also those that are handled.
        if (Debug)
        {
            AppDomain.CurrentDomain.FirstChanceException += (_, e) => Note(e.Exception);
            DebugSettings.XamlResourceReferenceFailed += (_, e) => Note(new Exception("resource: " + e.Message));
            DebugSettings.BindingFailed += (_, e) => Note(new Exception("binding: " + e.Message));
        }
        L.Use(System.Globalization.CultureInfo.CurrentUICulture.TwoLetterISOLanguageName);
    }

    public static bool Debug = Environment.GetCommandLineArgs().Contains("--debug");

    /// Where the window keeps its settings and log: the folder FlClash.exe
    /// names, which is beside the program in a portable copy.
    public static readonly string DataDir = DataFolder();

    static string DataFolder()
    {
        var argv = Environment.GetCommandLineArgs();
        int i = Array.IndexOf(argv, "--data");
        if (i >= 0 && i + 1 < argv.Length && argv[i + 1].Length > 0) return argv[i + 1];
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

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        try { Launch(); }
        catch (Exception e) { Note(e); throw; }
    }

    void Launch()
    {
        var argv = Environment.GetCommandLineArgs();
        string? Arg(string name)
        {
            int i = Array.IndexOf(argv, name);
            return i >= 0 && i + 1 < argv.Length ? argv[i + 1] : null;
        }
        var pipe = Arg("--pipe") ?? Agent.DefaultPipe();
        var page = Arg("--page");

        var agent = Agent.Connect(pipe, 3000, out bool foreign);
        if (agent == null)
        {
            MessageBox(IntPtr.Zero, foreign
                ? L.T("Another program answered in place of FlClash. Nothing was sent to it.")
                : L.T("FlClash is not running. Start FlClash, then open its window from the icon in the notification area."),
                "FlClash", 0x10);
            Exit();
            return;
        }
        // One window: a second start asks the agent to bring the first one up.
        single = new Mutex(true, @"Local\FlClash.Window", out bool first);
        if (!first)
        {
            agent.Open(page);
            agent.Finish(2000);
            Exit();
            return;
        }
        window = new MainWindow(agent, page);
        agent.Start(window.DispatcherQueue);
        agent.Hello();
        agent.Watch(true);
        window.Activate();
        window.Front();
    }

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    static extern int MessageBox(IntPtr hwnd, string text, string caption, uint type);
}
