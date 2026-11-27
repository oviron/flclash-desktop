# flclash-desktop

Desktop FlClash for macOS, Windows and Linux: a Go engine around the mihomo core
with a native window per system on top. GPL-3.0. The Android app lives in
`oviron/FlClash`; this repository shares no code with it yet.

What exists now is the engine without privileges: import of servers, routing
policies, the config compiler and the core embedded in `flclashctl`. TUN, the
privileged helper, the tray and the windows are not built.

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
- The product captures traffic through TUN and opens no local port.
  `-local-port` exists to check the core without privileges.
- Public files are in English and carry no private context.

## Layout

- `internal/ingest` — what the user pastes → servers in mihomo's proxy shape.
- `internal/policy` — routing policy documents.
- `internal/compile` — server + policy → the config the core runs.
- `internal/core` — mihomo inside this process.
- `internal/store` — state on disk; `$FLCLASH_HOME` overrides the directory.
- `cmd/flclashctl` — the command line.

## Checks

```sh
gofmt -l .
go vet ./...
go test ./...
go build -tags with_gvisor ./cmd/flclashctl
```
