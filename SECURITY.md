# Security

## Reporting a problem

Report a vulnerability privately through
[GitHub's private reporting](https://github.com/oviron/flclash-desktop/security/advisories/new),
not in a public issue. Say what is affected, how to reproduce it and what it
lets an attacker do.

Only the latest release is fixed. Problems of the core itself belong to
[mihomo](https://github.com/MetaCubeX/mihomo) unless they come from the
`compat` layer of [oviron/mihomo](https://github.com/oviron/mihomo).

## What runs with privileges

The port and the system proxy run in the app, as the user. Only TUN needs
administrator rights, and only through the helper: a launchd daemon on macOS,
a service on Windows. It listens on a socket or a named pipe that only the
system and the user it was installed for may open. It does not take a config:
it takes a server and rules, builds the core's config itself and runs the core
it was installed with. A server must have the shape the importer writes: a
known type, an address, a valid port, and no field outside the importer's list.
Rules go through the same parser as an imported policy, and a rule it does not
know is refused.

Settings, System helper, removes the helper through the system's prompt; so
does `flclashctl helper uninstall`, run with `sudo` on macOS or as
administrator on Windows.

## Builds

Release files are built by GitHub Actions from the tagged commit. Each has a
SHA-256 sum in `SHA256SUMS` and a build provenance attestation; the README
says how to check them. The apps are not signed by Apple or Microsoft yet.
