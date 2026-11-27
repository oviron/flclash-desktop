// swift-tools-version:5.9
import PackageDescription

// The engine is the Go library in Bridge/, built by `make bridge`.
let package = Package(
    name: "FlClash",
    platforms: [.macOS(.v14)],
    targets: [
        .target(name: "CFlClash"),
        .executableTarget(
            name: "FlClash",
            dependencies: ["CFlClash"],
            linkerSettings: [
                .unsafeFlags(["-LBridge", "-lflclash"]),
                .linkedFramework("CoreFoundation"),
                .linkedFramework("Security"),
                .linkedLibrary("resolv"),
            ]
        ),
    ]
)
