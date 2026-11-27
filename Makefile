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
	cp flclashctl flclash-helper flclash-core dist/FlClash.app/Contents/MacOS/
	# SwiftPM stamps the binary with the deployment target as its SDK, and the
	# system then draws the app the old way. Put the SDK it was built with.
	vtool -set-build-version macos 15.0 $$(xcrun --show-sdk-version) -replace \
		-output dist/FlClash.app/Contents/MacOS/FlClash macos/.build/release/FlClash
	cp macos/Resources/Info.plist dist/FlClash.app/Contents/
	cp -R macos/Resources/AppIcon.icns l10n/en.lproj l10n/ru.lproj dist/FlClash.app/Contents/Resources/
	codesign --force --deep --sign - dist/FlClash.app

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...
