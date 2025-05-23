import Foundation

enum AppError: Error, LocalizedError {
    case ioError(String)
    case jsonDecodingError(Error)
    case jsonEncodingError(Error)
    case networkError(Error)
    case apiError(String)
    case authenticationFailed(String)
    case tokenNotFound
    case clientSecretNotFound
    case clientSecretInvalid(String)
    case timedOut(String)
    case noSpamMessagesFound
    case invalidDate(String)
    case operationCancelled

    var errorDescription: String? {
        switch self {
        case .ioError(let msg): return "I/O Error: \(msg)"
        case .jsonDecodingError(let err): return "JSON Decoding Error: \(err.localizedDescription)"
        case .jsonEncodingError(let err): return "JSON Encoding Error: \(err.localizedDescription)"
        case .networkError(let err): return "Network Error: \(err.localizedDescription)"
        case .apiError(let msg): return "API Error: \(msg)"
        case .authenticationFailed(let msg): return "Authentication Failed: \(msg)"
        case .tokenNotFound: return "OAuth token not found."
        case .clientSecretNotFound: return "Client credentials.json not found."
        case .clientSecretInvalid(let msg): return "Client credentials.json invalid: \(msg)"
        case .timedOut(let msg): return "Operation timed out: \(msg)"
        case .noSpamMessagesFound: return "No spam messages found."
        case .invalidDate(let dateStr): return "Invalid date string: \(dateStr)"
        case .operationCancelled: return "Operation was cancelled."
        }
    }
}
