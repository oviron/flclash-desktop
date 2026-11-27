# flclash-desktop

A native desktop proxy client for macOS and Windows, built around the
[mihomo](https://github.com/MetaCubeX/mihomo) core. Each system gets its own
native app: AppKit on macOS, WinUI 3 on Windows. The two look and behave the
same and share one Go engine underneath.

This is an independent project. It is not a build of
[chen08209/FlClash](https://github.com/chen08209/FlClash), which is a Flutter
app with its own desktop builds, and it shares no code with it. It runs the same
core as the Android app [oviron/FlClash](https://github.com/oviron/FlClash): the
`compat` layer of [oviron/mihomo](https://github.com/oviron/mihomo).

Status: early, used daily by its author. There are no signed builds or
installers yet; build it from source. Linux is planned.

## What it does

- Imports servers from share links (VLESS, Hysteria2, TUIC and more), base64
  subscriptions, Clash configs and Throne entries. An entry with a field the
  importer does not understand is refused with the field named, never imported
  with the setting silently dropped.
- Keeps subscriptions up to date, measures servers, and shows where each one lets
  traffic out.
- Routes by policy: everything through the server, local network direct, or a
  policy of your own in mihomo's rule syntax.
- Three modes:
  - **Port only** (the default): a local HTTP/SOCKS port, nothing else changes.
  - **System proxy**: the port, with the system proxy pointed at it.
  - **TUN**: all traffic of the computer, with an optional guard that lets
    nothing leave past the tunnel, also when the core dies.
- Lives in the menu bar or the notification area, with a global shortcut and
  start at login. English and Russian.

## How it runs

The app runs the port and the system proxy itself: the core is a child process
of the app, running as you, and quitting the app turns the connection off. Only
TUN needs administrator rights. The first time you pick TUN, the app installs a
small privileged helper (a launchd daemon on macOS, a service on Windows) through
the system's prompt. The helper accepts only servers and rules the app's own
checks produce, and builds the core's config itself.

On macOS the window and the menu bar item are one process that links the engine
as a library. On Windows `FlClash.exe` holds the engine and the tray icon, and
the window is a separate process that exists only while it is open.

## Building

macOS, with Go and the Command Line Tools:

```sh
make check   # gofmt, go vet, go test
make app     # dist/FlClash.app, ad hoc signed
```

Windows, with Go and the .NET 10 SDK:

```powershell
windows\build.ps1   # everything into dist\windows\FlClash
```

`flclashctl` is the command line: import, list, select, policies, and the
tunnel. Run it without arguments for the full list.

`AGENTS.md` describes the layout, the constraints and the checks in more detail.

## License

GPL-3.0, as mihomo. See `LICENSE`.
