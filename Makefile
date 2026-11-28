# Builds the engine tools and the macOS app.

# The version comes from the last v* tag: v0.2.0 builds 0.2.0, a commit after it
# 0.2.0-3-gabc1234. The bundle takes the numbers only, and its build number is
# the count of commits, so each build is newer than the one before.
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' 2>/dev/null | sed 's/^v//')
ifeq ($(VERSION),)
VERSION := 0.0.0-dev
endif
SHORT := $(firstword $(subst +, ,$(subst -, ,$(VERSION))))
BUILD := $(shell git rev-list --count HEAD 2>/dev/null || echo 1)
# The core leaves out the outbounds the importer never produces: EasyTier and
# ZeroTier, about 12 MB of the core. The tunnel runs on the mips stack, which
# needs no gVisor, so with_gvisor (and Tailscale with it) stays out too.
CORE_TAGS := no_easytier,no_zerotier
# The release workflow sets CHANNEL=release: only such a copy updates itself.
CHANNEL ?=
RELEASE := -trimpath -ldflags "-s -w -X github.com/oviron/flclash-desktop/internal/build.Version=$(VERSION) -X github.com/oviron/flclash-desktop/internal/build.Channel=$(CHANNEL)"

.PHONY: tools bridge app check version sign-release

# The command line, the privileged supervisor and the core it runs.
tools:
	go build $(RELEASE) -o flclashctl ./cmd/flclashctl
	go build -tags $(CORE_TAGS) $(RELEASE) -o flclash-core ./cmd/flclash-core
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
	plutil -replace CFBundleShortVersionString -string $(SHORT) dist/FlClash.app/Contents/Info.plist
	plutil -replace CFBundleVersion -string $(BUILD) dist/FlClash.app/Contents/Info.plist
	cp -R macos/Resources/AppIcon.icns l10n/en.lproj l10n/ru.lproj dist/FlClash.app/Contents/Resources/
	codesign --force --deep --sign - dist/FlClash.app

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test ./...

version:
	@echo $(VERSION)

# Signs the sums of a published release with the key in this Mac's keychain
# and adds the signature to the release: the app installs only what it signs.
# The sums are signed only once GitHub attests the release workflow made them
# from the tag.
# The key never leaves the keychain for CI.  make sign-release TAG=v0.4.0
sign-release:
	@test -n "$(TAG)" || { echo "make sign-release TAG=vX.Y.Z"; exit 1; }
	mkdir -p dist/sign
	gh release download $(TAG) -R oviron/flclash-desktop -p SHA256SUMS -D dist/sign --clobber
	gh attestation verify dist/sign/SHA256SUMS -R oviron/flclash-desktop \
		--signer-workflow oviron/flclash-desktop/.github/workflows/release.yml --source-ref refs/tags/$(TAG)
	@FLCLASH_SIGNING_KEY="$$(security find-generic-password -a flclash-desktop -s 'FlClash release signing key' -w)" \
		go run ./cmd/sign-release dist/sign/SHA256SUMS
	gh release upload $(TAG) -R oviron/flclash-desktop --clobber dist/sign/SHA256SUMS.sig
