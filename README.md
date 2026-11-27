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

Status: early, used daily by its author. Linux is planned.

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

## Installing

Download the files from the
[latest release](https://github.com/oviron/flclash-desktop/releases/latest).

**macOS** 15 or later, Apple silicon: open `FlClash-<version>-macos-arm64.dmg`
and drag FlClash to Applications. The app is not signed by Apple yet, so the
first start is refused. Open it once, then go to System Settings, Privacy &
Security, and click Open Anyway. Or, in Terminal:

```sh
xattr -dr com.apple.quarantine /Applications/FlClash.app
```

**Windows** 10 2004 or later, x64 or Arm64: run
`FlClash-<version>-windows-<arch>-setup.exe`. It installs for the current user
and needs no administrator rights. The files are not signed by Microsoft yet,
so SmartScreen may stop the first start: click More info, then Run anyway.

**Portable, Windows**: unpack `FlClash-<version>-windows-<arch>-portable.zip`
anywhere, a USB stick included, and run `FlClash.exe`. A file named `portable`
beside it keeps the servers and settings in the `data` folder next to it, so
the folder can be carried to another computer. That folder holds the keys of
your servers: keep it where others cannot read it. TUN works as in the
installed app: the first time it is turned on, on each computer, the system
asks for an administrator and installs the helper. One FlClash runs at a
time: while the installed app runs, a portable copy says so and does not start,
and the other way round.

The helper stays on the computer when the app is gone. To remove it, open
Settings, System helper, and click Remove, with the connection off; do this
before you remove the app or leave a computer with a portable copy. From the
command line:

```sh
sudo /Applications/FlClash.app/Contents/MacOS/flclashctl helper uninstall
```

On Windows, in PowerShell as administrator, from the app's folder:

```powershell
.\flclashctl.exe helper uninstall
```

### Checking a download

Every release lists the SHA-256 sum of each file in `SHA256SUMS`, and GitHub
keeps a build provenance attestation of each: a signed record of the workflow
and the commit it was built from.

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
gh attestation verify FlClash-<version>-macos-arm64.dmg -R oviron/flclash-desktop
```

When a release also carries `SHA256SUMS.asc`, the sums are signed with the key
`1139 C91B 6525 883E 6783 DCF0 4A94 DA48 8A4C 5033`:

```sh
gpg --verify SHA256SUMS.asc SHA256SUMS
```

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

The version comes from the last `v*` git tag.

Windows, with Go and the .NET 10 SDK:

```powershell
windows\build.ps1   # everything into dist\windows\FlClash
```

`flclashctl` is the command line: import, list, select, policies, and the
tunnel. Run it without arguments for the full list.

`AGENTS.md` describes the layout, the constraints and the checks in more detail.

## Releasing

Add the release's section to `CHANGELOG.md`, then tag the commit:

```sh
git tag -a v0.2.0 -m v0.2.0 && git push origin v0.2.0
```

The release workflow builds both systems, writes the sums and the
attestations, and publishes the release with the notes from the changelog. A
version with a hyphen, such as `v0.2.0-rc.1`, is published as a pre-release.

## License

GPL-3.0, as mihomo. See `LICENSE`.
