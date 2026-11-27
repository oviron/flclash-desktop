global using System;
global using System.Collections.Generic;
global using System.IO;
global using System.Linq;
global using System.Threading;
global using System.Threading.Tasks;

using System.Runtime.InteropServices;

namespace System.Runtime.CompilerServices
{
    // What newer C# needs from the framework and 4.8 does not have.
    internal static class IsExternalInit { }
}

namespace FlClash
{
    internal static class Compat
    {
        public static TValue? GetValueOrDefault<TKey, TValue>(this Dictionary<TKey, TValue> d, TKey key) where TKey : notnull =>
            d.TryGetValue(key, out var v) ? v : default;

        /// Names compared as Explorer sorts them: "server 2" before "server 10".
        public sealed class Natural : IComparer<string>
        {
            public int Compare(string? a, string? b) => StrCmpLogicalW(a ?? "", b ?? "");
        }

        [DllImport("shlwapi.dll", CharSet = CharSet.Unicode)]
        static extern int StrCmpLogicalW(string a, string b);
    }
}
