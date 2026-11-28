using System.Collections.Concurrent;
using System.IO.Pipes;
using System.Runtime.InteropServices;
using System.Security.Principal;
using System.Text;
using System.Windows.Threading;
using Microsoft.Win32.SafeHandles;

namespace FlClash;

/// The agent in the notification area: it holds the engine and the state,
/// and this window draws what it sends and sends what the person does. One
/// JSON object per line each way.
public sealed class Agent
{
    readonly NamedPipeClientStream pipe;
    // Lines wait here and go out one after another on their own thread: the
    // window's thread never waits for the pipe.
    readonly BlockingCollection<string> outbox = new();
    readonly object gate = new();
    readonly Dictionary<int, TaskCompletionSource<Reply>> pending = new();
    int nextId;
    bool gone;
    Dispatcher? queue;

    public Snapshot S { get; private set; } = new();
    public AppInfo App { get; private set; } = new();

    /// The state changed. Called on the window's thread.
    public event Action? Changed;
    /// The agent asks the window to come to the front, at a page if given.
    public event Action<string?>? Activate;
    /// The agent is gone: the app was quit.
    public event Action? Lost;

    readonly Thread writing;

    Agent(NamedPipeClientStream pipe)
    {
        this.pipe = pipe;
        writing = new Thread(Write) { IsBackground = true, Name = "agent out" };
        writing.Start();
    }

    /// Sends what is waiting and stops; for a window that only passes a wish on.
    public void Finish(int timeoutMs)
    {
        outbox.CompleteAdding();
        writing.Join(timeoutMs);
    }

    /// The pipe of the agent of the person signed in: only they may open it.
    public static string DefaultPipe()
    {
        var sid = WindowsIdentity.GetCurrent().User?.Value ?? "";
        return "flclash-app-" + sid;
    }

    /// Connects to the agent. The name of the pipe can be guessed, so another
    /// program could have taken it first: the process that serves it must run
    /// as the person signed in here, or nothing is sent to it.
    public static Agent? Connect(string name, int timeoutMs, out bool foreign)
    {
        foreign = false;
        const string prefix = @"\\.\pipe\";
        if (name.StartsWith(prefix, StringComparison.OrdinalIgnoreCase)) name = name.Substring(prefix.Length);
        // Asynchronous: a read that waits must not hold up a write on the same pipe.
        var pipe = new NamedPipeClientStream(".", name, PipeDirection.InOut, PipeOptions.Asynchronous);
        try
        {
            pipe.Connect(timeoutMs);
        }
        catch (Exception)
        {
            pipe.Dispose();
            return null;
        }
        var mine = WindowsIdentity.GetCurrent().User?.Value;
        if (mine == null || ServerUser(pipe.SafePipeHandle) != mine)
        {
            foreign = true;
            pipe.Dispose();
            return null;
        }
        return new Agent(pipe);
    }

    /// The user the process at the other end of the pipe runs as.
    static string? ServerUser(SafePipeHandle handle)
    {
        if (!GetNamedPipeServerProcessId(handle, out uint pid)) return null;
        var process = OpenProcess(0x1000 /* PROCESS_QUERY_LIMITED_INFORMATION */, false, pid);
        if (process == IntPtr.Zero) return null;
        try
        {
            if (!OpenProcessToken(process, 0x0008 /* TOKEN_QUERY */, out var token)) return null;
            try
            {
                GetTokenInformation(token, 1 /* TokenUser */, IntPtr.Zero, 0, out int size);
                var buffer = Marshal.AllocHGlobal(size);
                try
                {
                    if (!GetTokenInformation(token, 1, buffer, size, out _)) return null;
                    // TOKEN_USER starts with the SID's address.
                    var sid = Marshal.ReadIntPtr(buffer);
                    return new SecurityIdentifier(sid).Value;
                }
                finally { Marshal.FreeHGlobal(buffer); }
            }
            finally { CloseHandle(token); }
        }
        finally { CloseHandle(process); }
    }

    [DllImport("kernel32.dll", SetLastError = true)] static extern bool GetNamedPipeServerProcessId(SafePipeHandle pipe, out uint pid);
    [DllImport("kernel32.dll", SetLastError = true)] static extern IntPtr OpenProcess(uint access, bool inherit, uint pid);
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    [DllImport("advapi32.dll", SetLastError = true)] static extern bool OpenProcessToken(IntPtr process, uint access, out IntPtr token);
    [DllImport("advapi32.dll", SetLastError = true)] static extern bool GetTokenInformation(IntPtr token, int cls, IntPtr info, int size, out int needed);

    /// Reads what the agent sends until it goes away.
    public void Start(Dispatcher queue)
    {
        this.queue = queue;
        var thread = new Thread(Read) { IsBackground = true, Name = "agent in" };
        thread.Start();
    }

    void Post(Action action)
    {
        var q = queue;
        if (q != null && !q.HasShutdownStarted) q.BeginInvoke(action);
    }

    void Write()
    {
        try
        {
            foreach (var line in outbox.GetConsumingEnumerable())
            {
                var bytes = Encoding.UTF8.GetBytes(line + "\n");
                pipe.Write(bytes, 0, bytes.Length);
                pipe.Flush();
            }
        }
        catch (Exception e) when (e is IOException or ObjectDisposedException or InvalidOperationException)
        {
            Drop();
        }
    }

    /// The pipe is closed or broken: nothing more goes out, and every
    /// question still waiting is answered with the reason.
    void Drop()
    {
        List<TaskCompletionSource<Reply>> waiting;
        lock (gate)
        {
            if (gone) return;
            gone = true;
            waiting = pending.Values.ToList();
            pending.Clear();
        }
        outbox.CompleteAdding();
        var lost = new Reply { Error = L.T("The connection to FlClash was lost.") };
        foreach (var w in waiting) w.TrySetResult(lost);
    }

    /// The window is closing: what it still waits for is let go.
    public void Close()
    {
        Drop();
        try { pipe.Dispose(); } catch (Exception) { }
    }

    void Read()
    {
        try
        {
            using var reader = new StreamReader(pipe, new UTF8Encoding(false));
            string? line;
            while ((line = reader.ReadLine()) != null)
            {
                var e = Json.Parse<AgentEvent>(line);
                if (e == null) continue;
                Post(() => Take(e));
            }
        }
        catch (IOException) { }
        catch (ObjectDisposedException) { }
        catch (InvalidOperationException) { }
        bool closing;
        lock (gate) closing = gone;
        Drop();
        if (!closing) Post(() => Lost?.Invoke());
    }

    void Take(AgentEvent e)
    {
        switch (e.Event)
        {
            case "model":
                if (e.Model != null) S = Whole(e.Model);
                if (e.App != null)
                {
                    App = e.App;
                    App.HotKey ??= new();
                    L.Use(App.Lang);
                }
                Changed?.Invoke();
                break;
            case "reply":
                TaskCompletionSource<Reply>? done;
                lock (gate)
                {
                    if (pending.TryGetValue(e.Id, out done)) pending.Remove(e.Id);
                }
                done?.TrySetResult(e.Reply ?? new Reply());
                break;
            case "activate":
                Activate?.Invoke(e.Page);
                break;
        }
    }

    /// A list the agent left out is an empty one, and so is a word.
    static Snapshot Whole(Snapshot s)
    {
        s.View ??= new();
        s.Status ??= new();
        s.View.Servers ??= new();
        s.View.Policies ??= new();
        s.View.Subscriptions ??= new();
        s.View.Selected ??= "";
        s.View.Policy ??= "";
        s.View.Mode ??= "proxy";
        s.View.Auth ??= "";
        s.View.Dns ??= "";
        s.Status.Server ??= "";
        s.Status.Policy ??= "";
        s.Status.Mode ??= "";
        s.Status.Guard ??= "";
        s.Status.GuardProblem ??= "";
        s.Delays ??= new();
        s.History ??= new();
        s.Updating ??= new();
        if (s.Stats != null) s.Stats.Connections ??= new();
        foreach (var sub in s.View.Subscriptions)
        {
            sub.Problem ??= "";
            sub.Host ??= "";
            sub.Agent ??= "";
            sub.Skip ??= "";
        }
        return s;
    }

    public void Send(Message m)
    {
        var line = Json.Write(m);
        try { outbox.TryAdd(line); }
        catch (InvalidOperationException) { }
    }

    /// Sends a message and waits for its reply, or for the pipe to go. The
    /// one who waits is the window: it goes on on its own thread.
    public Task<Reply> Ask(Message m)
    {
        var done = new TaskCompletionSource<Reply>(TaskCreationOptions.RunContinuationsAsynchronously);
        lock (gate)
        {
            if (gone) return Task.FromResult(new Reply { Error = L.T("The connection to FlClash was lost.") });
            m.Id = ++nextId;
            pending[m.Id] = done;
        }
        Send(m);
        return done.Task;
    }

    // An engine request on one of the agent's lines: "change" for what alters
    // what runs, "do" for the rest, "aside" for quick reads beside the
    // changes, "subscription" for what waits for a provider.
    public Task<Reply> Change(Request r, bool quiet = false) => Ask(new Message { Action = "change", Request = r, Quiet = quiet });
    public Task<Reply> Do(Request r, bool quiet = false) => Ask(new Message { Action = "do", Request = r, Quiet = quiet });
    public Task<Reply> Aside(Request r) => Ask(new Message { Action = "aside", Request = r });
    public Task<Reply> Subscription(Request r) => Ask(new Message { Action = "subscription", Request = r });

    public void Hello() => Send(new Message { Action = "hello" });
    public void Watch(bool on) => Send(new Message { Action = "watch", On = on });
    public void Toggle() => Send(new Message { Action = "toggle" });
    public void TurnOff() => Send(new Message { Action = "turnOff" });
    public void Refresh() => Send(new Message { Action = "refresh" });
    public void UpdateAll() => Send(new Message { Action = "updateAll" });
    public void Dismiss() => Send(new Message { Action = "dismiss" });
    public void Open(string? page) => Send(new Message { Action = "open", Page = page });
    public void ConnectTo(string server) => Send(new Message { Action = "connect", Server = server });
    public void Measure(IEnumerable<string> servers) => Send(new Message { Action = "measure", Servers = servers.ToList() });
    public Task<Reply> InstallHelper() => Ask(new Message { Action = "installHelper" });
    public Task<Reply> RemoveHelper() => Ask(new Message { Action = "removeHelper" });
    public Task<Reply> SetHotKey(HotKey key) => Ask(new Message { Action = "hotKey", HotKey = key });
    public Task<Reply> CheckUpdate() => Ask(new Message { Action = "checkUpdate" });
    public void SetAutoUpdate(bool on) => Send(new Message { Action = "autoUpdate", On = on });
    /// Fetches the newer release and runs its installer; the app quits for that.
    public Task<Reply> InstallUpdate() => Ask(new Message { Action = "installUpdate" });
    public Task<Reply> SetLogin(bool on) => Ask(new Message { Action = "login", On = on });

    // The settings, as the macOS model sends them: a value that is already
    // set is not sent again.
    public void SelectServer(string id) => _ = Change(new Request { Op = "select", Server = id });
    public void SelectPolicy(string name) => _ = Change(new Request { Op = "select", Policy = name });
    public Task<Reply> SetMode(string mode) => mode == S.View.Mode ? Task.FromResult(new Reply()) : Change(new Request { Op = "mode", Text = mode });
    public void SetPort(int port) { if (port != S.View.Port) _ = Change(new Request { Op = "port", Text = port.ToString() }); }
    public void SetLan(bool on) { if (on != S.View.Lan) _ = Change(new Request { Op = "lan", Text = on ? "on" : "off" }); }
    public void SetAuth(string auth) { if (auth != S.View.Auth) _ = Change(new Request { Op = "auth", Text = auth }); }
    public void SetDns(string dns) { if (dns != S.View.Dns) _ = Change(new Request { Op = "dns", Text = dns }); }
    public void SetProtect(bool on) => _ = Change(new Request { Op = "protect", Text = on ? "on" : "off" });
    public void SetAutoConnect(bool on) => _ = Do(new Request { Op = "autoConnect", Text = on ? "on" : "off" });
    public void CloseConnection(string id) => _ = Aside(new Request { Op = "close", Name = id });
}
