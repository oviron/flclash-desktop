# Builds the engine tools and the macOS app.
RELEASE := -trimpath -ldflags "-s -w"

.PHONY: tools bridge app check

# The command line, the privileged supervisor and the core it runs.
tools:
	go build -tags with_gvisor $(RELEASE) -o flclashctl ./cmd/flclashctl
	go build -tags with_gvisor $(RELEASE) -o flclash-core ./cmd/flclash-core
	go build $(RELEASE) -o flclash-helper ./cmd/flclash-helper

# The engine as a C library for the app to link.
bridge:
	mkdir -p macos/Bridge
	go build -buildmode=c-archive $(RELEASE) -o macos/Bridge/libflclash.a ./bridge

# The app carries the tools: its helper is installed from inside the bundle.
app: tools bridge
	cd macos && swift build -c release
	mkdir -p dist/FlClash.app/Contents/MacOS dist/FlClash.app/Contents/Resources
	cp macos/.build/release/FlClash flclashctl flclash-helper flclash-core dist/FlClash.app/Contents/MacOS/
	cp macos/Resources/Info.plist dist/FlClash.app/Contents/
	cp -R macos/Resources/en.lproj macos/Resources/ru.lproj dist/FlClash.app/Contents/Resources/
	codesign --force --deep --sign - dist/FlClash.app

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...
