// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "RatioConnector",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(name: "RatioConnector", path: "Sources/RatioConnector"),
        .testTarget(name: "RatioConnectorTests", dependencies: ["RatioConnector"], path: "Tests/RatioConnectorTests"),
    ]
)
