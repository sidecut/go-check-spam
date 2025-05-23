// The Swift Programming Language
// https://docs.swift.org/swift-book
//
// Swift Argument Parser
// https://swiftpackageindex.com/apple/swift-argument-parser/documentation

import ArgumentParser
import Foundation

@main
struct SwiftCheckSpam: AsyncParsableCommand {
    static var configuration = CommandConfiguration(
        abstract: "Checks Gmail for spam messages and provides a daily count."
    )

    @Option(name: .shortAndLong, help: "Timeout in seconds for fetching messages.")
    var timeout: Int = 60

    @Option(name: .shortAndLong, help: "Number of days to look back for spam.")
    var days: Int = 30

    @Flag(name: .long, help: "Enable debug output.")
    var debug: Bool = false

    private var cutoffDateString: String {
        let calendar = Calendar.current
        let cutoffDate = calendar.date(byAdding: .day, value: -days, to: Date())!
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter.string(from: cutoffDate)
    }

    enum OutputState {
        case firstLine, beforeDate, onOrAfterDate
    }

    func printSpamSummary(spamCounts: [String: Int]) throws {
        if spamCounts.isEmpty {
            print("No spam messages to summarize.")
            return
        }

        let dates = spamCounts.keys.sorted()

        var total = 0
        var outputState = OutputState.firstLine

        let inputFormatter = DateFormatter()
        inputFormatter.dateFormat = "yyyy-MM-dd"
        inputFormatter.timeZone = TimeZone(secondsFromGMT: 0)  // Dates from Gmail are effectively UTC based for day

        let outputDayFormatter = DateFormatter()
        outputDayFormatter.dateFormat = "EEE"  // Mon, Tue, etc.
        outputDayFormatter.timeZone = .current  // Display day of week in local timezone

        for dateStr in dates {
            let currentState: OutputState
            if dateStr < cutoffDateString {
                currentState = .beforeDate
            } else {
                currentState = .onOrAfterDate
            }

            if outputState == .beforeDate && currentState == .onOrAfterDate {
                print()  // Blank line separator
            }
            outputState = currentState

            let count = spamCounts[dateStr]!
            total += count

            guard let dateValue = inputFormatter.date(from: dateStr) else {
                throw AppError.invalidDate(dateStr)
            }
            let dayOfWeek = outputDayFormatter.string(from: dateValue)
            print("\(dayOfWeek) \(dateStr) \(count)")
        }
        print("Total: \(total)")
    }

    mutating func run() async throws {
        if debug {
            print("Debug mode enabled.")
            print("Looking back \(days) days.")
            print("Cutoff date: \(cutoffDateString)")
            print("Timeout: \(timeout) seconds.")
        }

        print("Attempting to authenticate and fetch spam for the past \(days) days.")
        print("Credentials expected at: credentials.json")
        print("Token cache will be at: token.json")

        let authenticator = GmailAuthenticator()
        let accessToken: String
        do {
            accessToken = try await authenticator.getAccessToken()
            if debug { print("Successfully obtained access token.") }
        } catch {
            print("Error during authentication: \(error.localizedDescription)")
            if debug, let appErr = error as? AppError { print(appErr) }
            throw ExitCode.failure
        }

        let gmailService = GmailService(accessToken: accessToken)

        do {
            let spamCounts = try await gmailService.getSpamCounts(
                cutoffDateString: cutoffDateString,
                timeout: timeout,
                debug: debug
            )

            if spamCounts.isEmpty && !debug {
                print("No spam messages found for the past \(days) days (based on internalDate).")
            } else {
                print(
                    "Spam email counts for the past \(days) days (based on internalDate, local timezone for day of week):"
                )
                try printSpamSummary(spamCounts: spamCounts)
            }
        } catch AppError.noSpamMessagesFound {
            print("No spam messages found for the past \(days) days (based on internalDate).")
        } catch {
            print("Error getting spam counts: \(error.localizedDescription)")
            if debug, let appErr = error as? AppError { print(appErr) }
            throw ExitCode.failure
        }
    }
}
