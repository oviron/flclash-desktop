using System.Globalization;
using System.Net.NetworkInformation;
using System.Net.Sockets;

namespace FlClash;

/// Numbers, times and places written for people, as the macOS app writes them.
public static class Format
{
    /// A number of bytes, counted in thousands as speeds and plans are.
    public static string Amount(long bytes)
    {
        var c = L.Culture;
        if (bytes < 1000) return L.N("%d bytes", (int)bytes);
        double v = bytes / 1000.0;
        if (v < 1000) return v.ToString("0", c) + " " + L.T("KB");
        v /= 1000;
        if (v < 1000) return v.ToString("0.#", c) + " " + L.T("MB");
        v /= 1000;
        if (v < 1000) return v.ToString("0.##", c) + " " + L.T("GB");
        return (v / 1000).ToString("0.##", c) + " " + L.T("TB");
    }

    public static string Rate(long bytes) => L.F("%@/s", Amount(bytes));

    /// A moment from the agent, or nothing for a time no calendar holds:
    /// a provider may send any number.
    public static DateTime? Local(long unix)
    {
        // A day off the ends, so that the local time stays inside the calendar too.
        if (unix < -62135596800 + 86400 || unix > 253402300799 - 86400) return null;
        return DateTimeOffset.FromUnixTimeSeconds(unix).LocalDateTime;
    }

    /// A moment: the time today, a day and a time otherwise.
    public static string? Moment(long unix)
    {
        if (Local(unix) is not { } at) return null;
        var time = at.ToString(L.Culture.DateTimeFormat.ShortTimePattern, L.Culture);
        if (at.Date == DateTime.Today) return time;
        if (at.Date == DateTime.Today.AddDays(-1)) return L.T("yesterday") + ", " + time;
        return at.ToString("d MMM", L.Culture) + ", " + time;
    }

    /// A day, as "27 Nov 2026".
    public static string? Day(long unix) => Local(unix)?.ToString("d MMM yyyy", L.Culture);

    /// How long ago, as "3 hours ago".
    public static string? Ago(long unix)
    {
        if (Local(unix) is not { } at) return null;
        var span = DateTime.Now - at;
        if (span.TotalMinutes < 60) return L.N("%d minutes ago", Math.Max(1, (int)span.TotalMinutes));
        if (span.TotalHours < 24) return L.N("%d hours ago", (int)span.TotalHours);
        return L.N("%d days ago", (int)Math.Min(span.TotalDays, int.MaxValue));
    }

    /// The name of a country in the language shown.
    public static string Country(string code)
    {
        // .NET names regions in its own language or the region's; Windows
        // names them in the language of the system, which the app follows.
        var buffer = new char[128];
        int n = GetGeoInfoEx(code.ToUpperInvariant(), GeoFriendlyName, buffer, buffer.Length);
        return n > 1 ? new string(buffer, 0, n - 1) : code.ToUpperInvariant();
    }

    const int GeoFriendlyName = 8;

    [System.Runtime.InteropServices.DllImport("kernel32.dll", CharSet = System.Runtime.InteropServices.CharSet.Unicode)]
    static extern int GetGeoInfoEx(string location, int geoType, char[] data, int length);

    /// The address of this computer in the local network, if it has one.
    public static string? LanAddress()
    {
        try
        {
            foreach (var nic in NetworkInterface.GetAllNetworkInterfaces())
            {
                if (nic.OperationalStatus != OperationalStatus.Up) continue;
                if (nic.NetworkInterfaceType is not (NetworkInterfaceType.Ethernet or NetworkInterfaceType.Wireless80211 or NetworkInterfaceType.GigabitEthernet)) continue;
                var props = nic.GetIPProperties();
                // A tunnel adapter has no gateway of its own on the LAN.
                if (props.GatewayAddresses.Count == 0) continue;
                foreach (var a in props.UnicastAddresses)
                {
                    if (a.Address.AddressFamily != AddressFamily.InterNetwork) continue;
                    var s = a.Address.ToString();
                    if (s.StartsWith("10.") || s.StartsWith("192.168.") || s.StartsWith("172.")) return s;
                }
            }
        }
        catch (NetworkInformationException) { }
        return null;
    }

    /// How long a connection has been open.
    public static string Age(long start)
    {
        var seconds = Math.Max(0, DateTimeOffset.UtcNow.ToUnixTimeSeconds() - start);
        if (seconds < 60) return L.F("%d s", seconds);
        if (seconds < 3600) return L.F("%d min", seconds / 60);
        return L.F("%d h", seconds / 3600);
    }

    /// The built-in policies have names for people; imported ones keep their own.
    public static string PolicyTitle(string name) => name switch
    {
        "all" => L.T("Everything through the server"),
        "lan-direct" => L.T("Local addresses direct"),
        "ru-direct" => L.T("Russia direct"),
        _ => name,
    };

    /// One line on what a policy does.
    public static string PolicyAbout(Policy p) => p.Name switch
    {
        "all" => L.T("All traffic goes through the server."),
        "lan-direct" => L.T("The home network and local addresses go directly, the rest through the server."),
        "ru-direct" => L.T("Russian sites go directly, the rest through the server."),
        _ => L.N("Imported, %d rules", p.Rules),
    };
}
