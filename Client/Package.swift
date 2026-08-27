// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "file-exploder",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(name: "file-exploder", targets: ["file-exploder"])
    ],
    dependencies: [],
    targets: [
        .executableTarget(
            name: "file-exploder",
            dependencies: [],
            path: "file-exploder"
        ),
        .testTarget(
            name: "file-exploderTests",
            dependencies: ["file-exploder"],
            path: "Tests"
        )
    ]
)
