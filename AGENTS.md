# flclash-desktop

Desktop FlClash for macOS, Windows and Linux: a Go engine around the mihomo core
with a native window per system on top. GPL-3.0. The Android app lives in
`oviron/FlClash`; this repository shares no code with it yet.

What exists now: import of servers, routing policies, the config compiler, a
privileged helper for macOS that runs the core with a TUN device, the command
line, and the first macOS app: a menu bar item and a window with servers and
routing. Windows and Linux have neither helper nor window yet.

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
- The window is native AppKit and stays light: no web view, no cross-platform
  toolkit. Closing it releases it; the app stays in the menu bar.
- The product captures traffic through TUN and opens no local port.
  `run -local-port` exists to check the core without privileges.
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
- `internal/helper` — the privileged supervisor and its protocol. It checks the
  server and the rules it is handed and builds the config itself; it links no
  core code.
- `internal/store` — state on disk; `$FLCLASH_HOME` overrides the directory.
- `internal/app` — what the product does; the command line and the windows call
  the same functions here.
- `cmd/flclashctl` — the command line. `cmd/flclash-helper` — the supervisor,
  root. `cmd/flclash-core` — the core process the supervisor runs.
- `bridge` — the engine as a C library: one JSON call in, one JSON reply out.
- `macos` — the AppKit app, a Swift package that links the bridge. Strings are
  keyed by their English text; `Resources/ru.lproj` holds the Russian.

## Checks

```sh
make check        # gofmt, go vet, go test
make app          # tools, bridge, Swift build, dist/FlClash.app (ad hoc signed)
```

The app needs only the Command Line Tools to build. Look at a change to the
window in the running app before calling it done: `open dist/FlClash.app`.
