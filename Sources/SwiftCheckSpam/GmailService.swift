import Foundation

final class GmailService: Sendable {
    private let accessToken: String
    private let session: URLSession
    private let baseURL = "https://www.googleapis.com/gmail/v1/users/me"

    init(accessToken: String, session: URLSession = .shared) {
        self.accessToken = accessToken
        self.session = session
    }

    private func makeRequest(url: URL, method: String = "GET") -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        return request
    }

    private func performRequest<T: Decodable>(request: URLRequest, debug: Bool) async throws -> T {
        if debug {
            print("Requesting: \(request.url?.absoluteString ?? "No URL")")
        }
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AppError.networkError(URLError(.badServerResponse))
        }

        if debug {
            print("Response status: \(httpResponse.statusCode)")
            if let body = String(data: data, encoding: .utf8) {
                print("Response body: \(body.prefix(500))...")
            }
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            do {
                let errorResponse = try JSONDecoder().decode(GmailErrorResponse.self, from: data)
                throw AppError.apiError(
                    "Gmail API Error: \(errorResponse.error.message) (Status: \(errorResponse.error.status ?? "N/A"), Code: \(errorResponse.error.code))"
                )
            } catch {  // If decoding GmailErrorResponse fails, throw a more generic error
                let errorBody = String(data: data, encoding: .utf8) ?? "Unknown API error content"
                throw AppError.apiError(
                    "Gmail API Error (Status \(httpResponse.statusCode)): \(errorBody)")
            }
        }
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw AppError.jsonDecodingError(error)
        }
    }

    func listSpamMessages(query: String, timeoutSeconds: Int, debug: Bool) async throws
        -> [GmailMessage]
    {
        var allMessages: [GmailMessage] = []
        var pageToken: String? = nil
        var totalFetchedMetadata = 0

        // Overall timeout for the listing and fetching operation
        return try await withTimeout(seconds: timeoutSeconds) {
            let messageStream = AsyncStream<GmailMessage> { continuation in
                Task {
                    defer { continuation.finish() }
                    do {
                        repeat {
                            var components = URLComponents(string: "\(self.baseURL)/messages")!
                            var queryItems = [
                                URLQueryItem(name: "labelIds", value: "SPAM"),
                                URLQueryItem(name: "q", value: query),
                                URLQueryItem(name: "maxResults", value: "100"),  // Adjust as needed
                            ]
                            if let pt = pageToken {
                                queryItems.append(URLQueryItem(name: "pageToken", value: pt))
                            }
                            components.queryItems = queryItems

                            guard let url = components.url else {
                                throw AppError.apiError("Invalid URL for listing messages")
                            }

                            let request = self.makeRequest(url: url)
                            let response: GmailListMessagesResponse = try await self.performRequest(
                                request: request, debug: debug)

                            if let messagesMetadata = response.messages {
                                totalFetchedMetadata += messagesMetadata.count
                                if debug {
                                    print(
                                        "\rFetched metadata for \(totalFetchedMetadata) messages...",
                                        terminator: "")
                                }

                                for metaMsg in messagesMetadata {
                                    // Fetch minimal message details (includes internalDate)
                                    // The Go code fetches "minimal" which is efficient.
                                    // Gmail API GET /messages/{id} with format=minimal
                                    guard
                                        let msgUrl = URL(
                                            string:
                                                "\(self.baseURL)/messages/\(metaMsg.id)?format=minimal"
                                        )
                                    else {
                                        if debug { print("Invalid URL for message \(metaMsg.id)") }
                                        continue
                                    }
                                    let msgRequest = self.makeRequest(url: msgUrl)
                                    // This part could be parallelized more effectively with TaskGroup
                                    // For simplicity here, fetching one by one within the stream producer
                                    // Or, collect all IDs then use TaskGroup.
                                    // The Go code uses goroutines per message ID.
                                    // Let's simulate that with detached tasks feeding the stream.
                                    Task.detached {  // Detached to not block the pagination loop
                                        do {
                                            // Implement retry logic here if needed for individual messages
                                            let fullMsg: GmailMessage =
                                                try await self.performRequest(
                                                    request: msgRequest, debug: debug)
                                            continuation.yield(fullMsg)
                                        } catch {
                                            if debug {
                                                print(
                                                    "Error fetching message \(metaMsg.id): \(error)"
                                                )
                                            }
                                            // Optionally yield an error or just skip
                                        }
                                    }
                                }
                            }
                            pageToken = response.nextPageToken
                        } while pageToken != nil
                    } catch {
                        if debug { print("Error in listSpamMessages stream: \(error)") }
                        continuation.finish()  // Ensure stream finishes on error
                        // Rethrowing here won't work as expected for the outer function.
                        // Error handling for the stream producer needs careful design.
                        // For now, errors in fetching are logged and skipped.
                    }
                }
            }

            for await message in messageStream {
                allMessages.append(message)
            }

            if debug { print() }  // Newline after progress

            if allMessages.isEmpty && totalFetchedMetadata == 0 {
                throw AppError.noSpamMessagesFound
            }
            return allMessages

        }  // End withTimeout
    }

    // Helper for timeout
    private func withTimeout<T: Sendable>(
        seconds: Int, operation: @escaping @Sendable () async throws -> T
    )
        async throws -> T
    {
        try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask {
                try await operation()
            }
            group.addTask {
                try await Task.sleep(nanoseconds: UInt64(seconds) * 1_000_000_000)
                throw AppError.timedOut("Operation exceeded \(seconds) seconds.")
            }
            guard let result = try await group.next() else {
                throw AppError.operationCancelled  // Should not happen if tasks are added
            }
            group.cancelAll()
            return result
        }
    }

    func getSpamCounts(cutoffDateString: String, timeout: Int, debug: Bool) async throws -> [String:
        Int]
    {
        var dailyCounts: [String: Int] = [:]
        let query = "after:\(cutoffDateString)"
        if debug { print("Gmail query: \(query)") }

        let messages = try await listSpamMessages(
            query: query, timeoutSeconds: timeout, debug: debug)

        if messages.isEmpty {
            print("No spam messages found after filtering.")
            return dailyCounts
        }

        let dateFormatter = DateFormatter()
        dateFormatter.dateFormat = "yyyy-MM-dd"
        dateFormatter.timeZone = .current  // Format based on local time zone

        for m in messages {
            guard let internalDateStr = m.internalDate, let internalDateMs = Int64(internalDateStr)
            else {
                if debug {
                    print("Warning: Missing or invalid internalDate for message ID \(m.id)")
                }
                continue
            }
            if internalDateMs <= 0 {
                if debug {
                    print(
                        "Warning: Invalid internalDate (\(internalDateMs)) for message ID \(m.id)")
                }
                continue
            }

            let emailTime = Date(timeIntervalSince1970: TimeInterval(internalDateMs) / 1000.0)
            let emailDateStr = dateFormatter.string(from: emailTime)
            dailyCounts[emailDateStr, default: 0] += 1
        }
        return dailyCounts
    }
}
