import Foundation

// For credentials.json
struct ClientSecret: Codable {
    struct Web: Codable {
        let clientID: String
        let projectID: String
        let authURI: String
        let tokenURI: String
        let authProviderX509CertURL: String
        let clientSecret: String
        let redirectURIs: [String]

        enum CodingKeys: String, CodingKey {
            case clientID = "client_id"
            case projectID = "project_id"
            case authURI = "auth_uri"
            case tokenURI = "token_uri"
            case authProviderX509CertURL = "auth_provider_x509_cert_url"
            case clientSecret = "client_secret"
            case redirectURIs = "redirect_uris"
        }
    }
    let web: Web?  // Make it optional if structure can vary (e.g. "installed")
    let installed: Web?  // Common for CLI apps
}

// For token.json
struct OAuthToken: Codable {
    let accessToken: String
    let refreshToken: String?
    let tokenType: String
    let expiryDate: Date?  // Store as Date, calculate from "expires_in"

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
        case tokenType = "token_type"
        case expiryDate  // Custom handling for expires_in
    }

    init(accessToken: String, refreshToken: String?, tokenType: String, expiresIn: Int?) {
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.tokenType = tokenType
        if let expiresIn = expiresIn {
            self.expiryDate = Date().addingTimeInterval(TimeInterval(expiresIn))
        } else {
            self.expiryDate = nil
        }
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        accessToken = try container.decode(String.self, forKey: .accessToken)
        refreshToken = try container.decodeIfPresent(String.self, forKey: .refreshToken)
        tokenType = try container.decode(String.self, forKey: .tokenType)

        // Handle "expires_in" by calculating expiryDate, or decode existing expiryDate
        if let expiresIn = try container.decodeIfPresent(
            Int.self, forKey: CodingKeys(stringValue: "expires_in") ?? .expiryDate)
        {
            self.expiryDate = Date().addingTimeInterval(TimeInterval(expiresIn))
        } else if let storedExpiry = try container.decodeIfPresent(Date.self, forKey: .expiryDate) {
            self.expiryDate = storedExpiry
        } else {
            self.expiryDate = nil
        }
    }

    func isExpired(gracePeriod: TimeInterval = 60.0) -> Bool {
        guard let expiry = expiryDate else { return false }  // If no expiry, assume not expired or handle as error
        return expiry.addingTimeInterval(-gracePeriod) < Date()
    }
}

// For Gmail API responses
struct GmailMessage: Codable, Identifiable {
    let id: String
    let threadId: String?
    let internalDate: String?  // Milliseconds since epoch as String
}

struct GmailListMessagesResponse: Codable {
    let messages: [GmailMessage]?
    let nextPageToken: String?
    let resultSizeEstimate: Int?
}

struct GmailErrorResponse: Codable, Error {
    struct ErrorDetail: Codable {
        let code: Int
        let message: String
        let status: String?
    }
    let error: ErrorDetail
}
