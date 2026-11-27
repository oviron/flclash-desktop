# flclash-desktop

Desktop FlClash for macOS, Windows and Linux: a Go engine around the mihomo core
with a native window per system on top. GPL-3.0. The Android app lives in
`oviron/FlClash`; this repository shares no code with it yet.

What exists now: import of servers, routing policies, the config compiler, a
privileged helper that runs the core for the tunnel (a launchd daemon on macOS,
a service on Windows), the command line, the macOS app (a menu bar item and a window), and
the Windows app (an icon in the notification area and a WPF window). Linux has
neither helper nor window yet.

Each system gets its own native app, and they look and behave alike. What can
be shared is shared: the engine, the strings (`l10n`), the icon (`assets`), and
on Windows the model the window draws, which is the Go twin of `Model.swift`.

## Constraints

- The core is the `compat` layer of `oviron/mihomo`, pinned by the `replace`
  lines in `go.mod`, the same core the phone runs. Do not move to another layer
  or to upstream without a decision.
- An import never drops a field it does not understand: it refuses the entry and
  says which field. A profile that connects with a setting missing is worse
  than one that does not import.
- A server keeps its ID across re-import and rename; selections refer to IDs.
- Entries and compiled configs hold credentials. Files are written `0600`,
  nothing prints an imported line, test fixtures use made-up hosts and keys.
- The windows are native — AppKit on macOS, WPF on Windows — and stay
  light: no web view, no cross-platform toolkit, no runtime of their own. The
  Windows window runs on the .NET Framework 4.8 every supported Windows
  carries; its styles are XAML read at start (`Theme.xaml`), the rest is built
  in code. Closing the window releases
  it; the app stays in the menu bar or the notification area. On Windows the
  window is a process of its own and exits when closed.
- Three modes: `proxy` (a local HTTP/SOCKS port only, the default), `system`
  (the port, and the system proxy pointed at it) and `tun` (the whole host).
  A change to one app's behavior is made in the other too, or noted as a gap.
- Only `tun` needs the helper, and it is installed the first time the person
  picks the tunnel. The app runs the other two modes itself, with the same
  `helper.Supervisor` (`helper.RunLocal`); `helper.call` sends each request to
  the side it belongs to. The connection lives with the app: quitting turns off
  every mode, and the core of a crashed app ends on its closed stdin. The
  command line has no supervisor of its own and drives the tunnel only.
- Nothing the unprivileged side sends reaches the root core as written: the
  helper accepts only the server shape `ingest` produces and rules `policy`
  produces. A new server field or rule type has to be added to those checks.
- "On" means the tunnel device exists, not that the core accepted a config.
- Public files are in English and carry no private context.

## Layout

- `internal/ingest` — what the user pastes → servers in mihomo's proxy shape.
- `internal/policy` — routing policy documents.
- `internal/compile` — server + policy → the config the core runs.
- `internal/core` — mihomo inside a process.
- `internal/helper` — the supervisor and its protocol: privileged in the
  helper for the tunnel, in the app for the other modes. It checks the server
  and the rules it is handed and builds the config itself; it links no core
  code.
- `internal/store` — state on disk; `$FLCLASH_HOME` overrides the directory, and a
  file named `portable` beside the program puts it in `data\` next to it.
- `internal/app` — what the product does; the command line and the windows call
  the same functions here.
- `cmd/flclashctl` — the command line. `cmd/flclash-helper` — the supervisor,
  root. `cmd/flclash-core` — the core process the supervisor runs.
- `internal/bridge` — the engine behind one call: a JSON request in, a JSON
  reply out. `bridge` exports it as a C library for the macOS app.
- `internal/model` — the state a front end draws and the rules for changing
  it, ported from `Model.swift`; `internal/agent` serves it to a window in
  another process, one JSON object per line.
- `internal/wfp`, `internal/winproxy` — the Windows firewall guard and the
  WinINet system proxy.
- `cmd/flclash-windows` — `FlClash.exe`: the icon in the notification area,
  the engine and the model; it runs the proxy modes, starts the window and
  installs the helper.
- `windows` — the WPF window, a C# project; it builds with the .NET SDK on any
  system (`dotnet build windows/FlClash.Window -c Release`).
- `macos` — the AppKit app, a Swift package that links the bridge.
- `l10n` — the strings of every app, keyed by their English text, in the
  macOS formats; `ru.lproj` holds the Russian. The Windows parts read the same
  files.
- `assets` — the app icon every system's icon is made from.

## Checks

```sh
make check        # gofmt, go vet, go test
make app          # tools, bridge, Swift build, dist/FlClash.app (ad hoc signed)
```

The app needs only the Command Line Tools to build. Look at a change to the
window in the running app before calling it done: `open dist/FlClash.app`.

On Windows, with Go and the .NET SDK:

```powershell
windows\build.ps1   # everything into dist\windows\FlClash; -SkipGo, -SkipWindow
```

`FlClash.exe` starts the window from `window\` beside it; the helper is
installed when the tunnel is first picked, or with `flclashctl helper install`
as administrator. Look at a window change in the running app on Windows too, and
keep the two apps saying and doing the same.

CI (`.github/workflows/ci.yml`) runs the Go checks on Linux, macOS and Windows,
govulncheck for the darwin and windows builds, gitleaks, and builds both apps.
A `v*` tag runs `release.yml`: the version comes from the tag, the notes from
the tag's section in `CHANGELOG.md`. Third-party actions are pinned by SHA.
