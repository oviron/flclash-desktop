using System.Web.Script.Serialization;

namespace FlClash;

// What the agent sends and takes, in the shapes of internal/agent,
// internal/model, internal/bridge and internal/app.

public sealed class Server
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string Group { get; set; } = "";
    public string Protocol { get; set; } = "";
    /// Where the server lets traffic out, a two-letter code, once it was asked.
    public string? Country { get; set; }
}

public sealed class Policy
{
    public string Name { get; set; } = "";
    public bool Builtin { get; set; }
    public int Rules { get; set; }
}

/// A group of servers read from an address. The address stays in the engine.
public sealed class Subscription
{
    public string Name { get; set; } = "";
    public string Host { get; set; } = "";
    public long Updated { get; set; }
    public long Used { get; set; }
    public long Total { get; set; }
    public long Expire { get; set; }
    public string Problem { get; set; } = "";
    public string Agent { get; set; } = "";
    public int Every { get; set; }
    public string Skip { get; set; } = "";
}

public sealed class EngineView
{
    public List<Server> Servers { get; set; } = new();
    public string Selected { get; set; } = "";
    public List<Policy> Policies { get; set; } = new();
    public string Policy { get; set; } = "";
    public bool Protect { get; set; } = true;
    /// "proxy", "system" or "tun".
    public string Mode { get; set; } = "proxy";
    public int Port { get; set; } = 7890;
    public bool AutoConnect { get; set; }
    public List<Subscription> Subscriptions { get; set; } = new();
    public bool Lan { get; set; }
    public string Auth { get; set; } = "";
    public string Dns { get; set; } = "";
}

public sealed class TunnelStatus
{
    public bool Installed { get; set; } = true;
    public bool Outdated { get; set; }
    public bool Running { get; set; }
    public bool Captured { get; set; }
    public string Server { get; set; } = "";
    public string Policy { get; set; } = "";
    public long Since { get; set; }
    public string Mode { get; set; } = "";
    public int Port { get; set; }
    /// The running port is open to the local network too.
    public bool Lan { get; set; }
    public string Guard { get; set; } = "";
    public string GuardProblem { get; set; } = "";
    public bool Blocked { get; set; }

    /// One line for people: what is true right now. Only the tunnel needs the
    /// helper, so only a mode that is not ready speaks of it.
    public string Headline(bool helperReady) =>
        !helperReady ? (Installed ? L.T("Helper needs an update") : L.T("Helper is not installed"))
        : Blocked ? (Mode == "system" ? L.T("The proxy is down") : L.T("Internet is blocked"))
        : !Running ? L.T("Off")
        : !Captured ? L.T("Not working")
        : L.T("Connected");
}

public sealed class Connection
{
    public string Id { get; set; } = "";
    public string Host { get; set; } = "";
    public int Port { get; set; }
    public string Network { get; set; } = "";
    public string Via { get; set; } = "";
    public string Rule { get; set; } = "";
    public long Upload { get; set; }
    public long Download { get; set; }
    public long Start { get; set; }
}

public sealed class Stats
{
    public long Up { get; set; }
    public long Down { get; set; }
    public long UpTotal { get; set; }
    public long DownTotal { get; set; }
    public List<Connection> Connections { get; set; } = new();
    public int Open { get; set; }
}

/// Where the running server lets traffic out: the address sites see.
public sealed class Exit
{
    public string Ip { get; set; } = "";
    public string? Country { get; set; }
}

public sealed class Delay
{
    /// "waiting", "ms" or "failed".
    public string State { get; set; } = "";
    public int Ms { get; set; }
    public string? Error { get; set; }
}

public sealed class Snapshot
{
    public EngineView View { get; set; } = new();
    public TunnelStatus Status { get; set; } = new();
    public bool Busy { get; set; }
    public string? Problem { get; set; }
    public Dictionary<string, Delay> Delays { get; set; } = new();
    public Stats? Stats { get; set; }
    public List<long[]> History { get; set; } = new();
    public Exit? Exit { get; set; }
    public List<string> Updating { get; set; } = new();
    public bool HelperReady { get; set; }
    public bool Test { get; set; }
    public bool Loaded { get; set; }

    public Server? SelectedServer => View.Servers.FirstOrDefault(s => s.Id == View.Selected);
}

public sealed class HotKey
{
    public uint Key { get; set; }
    public uint Modifiers { get; set; }
    public string Title { get; set; } = "";
}

public sealed class AppInfo
{
    public string Lang { get; set; } = "";
    public HotKey HotKey { get; set; } = new();
    public bool Login { get; set; }
    public string Version { get; set; } = "";
    public UpdateState? Update { get; set; }
}

/// Where updating stands, as internal/update keeps it.
public sealed class UpdateState
{
    public string Current { get; set; } = "";
    /// A copy built from a release tag: it can follow the releases.
    public bool Enabled { get; set; }
    public bool Auto { get; set; }
    public bool Checking { get; set; }
    public string? Latest { get; set; }
    public string? Page { get; set; }
    public long Checked { get; set; }
    /// This copy runs the new installer itself; otherwise its page is opened.
    public bool CanInstall { get; set; }
    public bool Installing { get; set; }
    public string? Error { get; set; }
}

public sealed class Failure
{
    public int Entry { get; set; }
    public string Reason { get; set; } = "";
}

public sealed class ImportResult
{
    public List<Server> Imported { get; set; } = new();
    public List<Failure> Failed { get; set; } = new();
}

/// A policy opened by the engine for reading or editing.
public sealed class PolicyDocument
{
    public string Name { get; set; } = "";
    public bool Builtin { get; set; }
    public string Text { get; set; } = "";
    public bool Listed { get; set; }
    public string Direct { get; set; } = "";
    public string Proxy { get; set; } = "";
    public string Block { get; set; } = "";
    public string Rest { get; set; } = "PROXY";
}

public sealed class Request
{
    public string Op { get; set; } = "";
    public string? Group { get; set; }
    public string? Text { get; set; }
    public string? Server { get; set; }
    public string? Policy { get; set; }
    public string? Name { get; set; }
    public string? Direct { get; set; }
    public string? Proxy { get; set; }
    public string? Block { get; set; }
    public string? Rest { get; set; }
    public string? Agent { get; set; }
    public string? Every { get; set; }
    public string? Skip { get; set; }
}

public sealed class Reply
{
    public string? Error { get; set; }
    public EngineView? View { get; set; }
    public TunnelStatus? Status { get; set; }
    public ImportResult? Imported { get; set; }
    public int Delay { get; set; }
    public Stats? Stats { get; set; }
    public string? Link { get; set; }
    public Exit? Exit { get; set; }
    public string? Log { get; set; }
    public PolicyDocument? Document { get; set; }
}

public sealed class Message
{
    public int Id { get; set; }
    public string Action { get; set; } = "";
    public string? Server { get; set; }
    public List<string>? Servers { get; set; }
    public bool On { get; set; }
    public string? Page { get; set; }
    public bool Quiet { get; set; }
    public Request? Request { get; set; }
    public HotKey? HotKey { get; set; }
}

public sealed class AgentEvent
{
    public int Id { get; set; }
    public string Event { get; set; } = "";
    public Snapshot? Model { get; set; }
    public AppInfo? App { get; set; }
    public Reply? Reply { get; set; }
    public string? Page { get; set; }
}

/// The window's own memory: where it was and how its lists are shown.
public sealed class WindowPrefs
{
    public int X { get; set; } = int.MinValue;
    public int Y { get; set; } = int.MinValue;
    public int Width { get; set; }
    public int Height { get; set; }
    public string ServerTab { get; set; } = "";
    public string ServerOrder { get; set; } = "added";
}

/// JSON as the framework reads and writes it, with no library to carry. It
/// writes the names as they are here and reads them in any case; the agent
/// reads them in any case too.
public static class Json
{
    static JavaScriptSerializer Serializer() => new() { MaxJsonLength = int.MaxValue, RecursionLimit = 64 };

    /// Null for text that is not JSON of that shape.
    public static T? Parse<T>(string text) where T : class
    {
        try { return Serializer().Deserialize<T>(text); }
        catch (Exception e) when (e is ArgumentException or InvalidOperationException or FormatException or OverflowException) { return null; }
    }

    public static string Write(object value) => Serializer().Serialize(value);
}
