# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/). The release
workflow takes the notes of a release from its section here.

## [Unreleased]

## [0.4.1] - 2026-11-28

### Added

- Pasting, importing or dropping a subscription URL (`http://` or `https://`)
  opens the Add a Subscription dialog with the address filled in, instead of
  refusing the link.
- Subscriptions without an explicit name take the provider's `Profile-Title`
  (plain or `base64:`) or `Content-Disposition` filename before falling back
  to the host name.
- Import accepts JSON arrays of Xray outbound objects (`vnext` / `users`,
  `publicKey`, WebSocket) and pretty-printed single JSON profiles.
- Share links and Xray profiles accept the `qq`, `360` and `randomized` uTLS
  fingerprints the core already supports.

### Fixed

- `flclashctl import` keeps line indentation, so Clash YAML files import
  cleanly from the command line too.
- On macOS, Import from File allows selecting `.yaml`, `.yml` and `.conf`
  files.

## [0.4.0] - 2026-11-28

### Added

- Updates from GitHub: FlClash looks for a newer release once a day, or when
  asked in Settings, and shows it in Settings and in the menu. The app in a
  folder it can write to (macOS) and the installed copy (Windows) download it,
  check its Ed25519 signature and sum, put it in place and restart; a portable
  copy opens the release page.

## [0.3.0] - 2026-11-28

### Changed

- The core leaves out the EasyTier, ZeroTier and Tailscale outbounds and the
  gVisor stack, none of which the app uses: it is 39 MB instead of 64 on macOS
  and starts with about 6 MB less memory.

- The Windows window is WPF on the .NET Framework that Windows carries, in
  place of WinUI 3 with its own .NET and Windows App SDK: it is about 1 MB
  instead of 85, takes about a third less memory while open, and one build
  serves x64 and Arm64. The installer removes the earlier window's files.

### Removed

- `flclashctl run`: the app runs the local port itself. The command line no
  longer carries a copy of the core and is 7 MB instead of 64.

## [0.2.0] - 2026-11-28

### Added

- A portable build for Windows: a zip whose copy keeps its servers and
  settings in a `data` folder beside it. TUN installs the helper on each
  computer as the installed app does.
- Settings, System helper: removes the helper, and what it keeps, through the
  system's prompt.

### Changed

- The plain Windows zip is replaced by the portable one.
- One FlClash runs at a time: a copy from another folder says where FlClash
  runs from and does not start.
- Installing and removing the helper go one at a time; removing waits until
  nothing runs and no guard holds.

### Fixed

- On macOS, removing the helper says so when the guard or the system proxy
  could not be put back.

## [0.1.0] - 2026-11-28

The first public release.

### Added

- Native apps for macOS (AppKit, Apple silicon, macOS 15 or later) and
  Windows (WinUI 3, x64 and Arm64, Windows 10 2004 or later) on one Go engine
  and the `compat` core of oviron/mihomo.
- Import from share links (VLESS, Hysteria2, TUIC and more), base64
  subscriptions, Clash configs and Throne entries. An entry with a field the
  importer does not understand is refused with the field named.
- Subscriptions with their own update interval, user agent and skip list.
- Server delay checks and the exit country of each server.
- Routing policies: everything through the server, local network direct, or
  your own in mihomo's rule syntax, with an editor for direct, proxy and block
  lists.
- Three modes: a local HTTP/SOCKS port, the system proxy, and TUN with an
  optional guard that lets nothing leave past the tunnel, also when the core
  dies.
- The port and the system proxy run in the app as the user. Only TUN needs the
  privileged helper, installed through the system's prompt the first time TUN
  is chosen.
- Menu bar and notification area icons, a global shortcut, start at login,
  English and Russian.
- `flclashctl`, the command line for the same engine.
- Releases with SHA-256 sums and build provenance attestations; a Windows
  installer for the current user.

[Unreleased]: https://github.com/oviron/flclash-desktop/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/oviron/flclash-desktop/releases/tag/v0.4.1
[0.4.0]: https://github.com/oviron/flclash-desktop/releases/tag/v0.4.0
[0.3.0]: https://github.com/oviron/flclash-desktop/releases/tag/v0.3.0
[0.2.0]: https://github.com/oviron/flclash-desktop/releases/tag/v0.2.0
[0.1.0]: https://github.com/oviron/flclash-desktop/releases/tag/v0.1.0
