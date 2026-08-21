// swift-tools-version: 5.10

import PackageDescription

let package = Package(
    name: "ConductorApp",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .executable(name: "Conductor", targets: ["ConductorApp"]),
    ],
    targets: [
        .executableTarget(
            name: "ConductorApp",
            path: "Sources/ConductorApp"
        ),
        .testTarget(
            name: "ConductorAppTests",
            dependencies: ["ConductorApp"],
            path: "Tests/ConductorAppTests"
        ),
    ],
    swiftLanguageVersions: [.v5]
)
