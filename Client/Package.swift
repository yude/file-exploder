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
    dependencies: [
        .package(url: "https://github.com/Kitura/BlueSSLService.git", from: "2.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "file-exploder",
            dependencies: [
                .product(name: "SSLService", package: "BlueSSLService")
            ],
            path: "file-exploder"
        )
    ]
)
