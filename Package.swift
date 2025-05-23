// swift-tools-version:5.7
// The swift-tools-version declares the minimum version of Swift required to build this package.

import PackageDescription

let package = Package(
    name: "SwiftCheckSpam",
    platforms: [
        .macOS(.v12) // Requires macOS 12+ for async/await and other modern Swift features
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.2.0"),
        // For a robust local HTTP server for OAuth, consider a library like:
        // .package(url: "https://github.com/httpswift/swifter.git", from: "1.5.0") // Example: Swifter
        // .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.9.0") // For HTTP client needs beyond URLSession
    ],
    targets: [
        .executableTarget(
            name: "SwiftCheckSpam",
            dependencies: [
                .product(name: "ArgumentParser", package: "swift-argument-parser"),
                // If using Swifter: .product(name: "Swifter", package: "swifter"),
            ]),
        .testTarget(
            name: "SwiftCheckSpamTests",
            dependencies: ["SwiftCheckSpam"]),
    ]
)