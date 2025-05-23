import Combine  // For future use or if using a library that leverages it
import Foundation

#if canImport(AppKit)  // For NSWorkspace
    import AppKit
#endif

class GmailAuthenticator {
    private let credentialsFilePath: String
    private let tokenFilePath: String
    private var clientSecret: ClientSecret.Web?
    private var currentToken: OAuthToken?

    private let tokenURL = URL(string: "https://oauth2.googleapis.com/token")!
    private let authURLBase = "https://accounts.google.com/o/oauth2/v2/auth"
    private let redirectURI = "http://127.0.0.1:8080/oauth2callback"  // Must match one in credentials.json
    private let gmailScope = "https://www.googleapis.com/auth/gmail.readonly"

    init(credentialsFilePath: String = "credentials.json", tokenFilePath: String = "token.json") {
        self.credentialsFilePath = credentialsFilePath
        self.tokenFilePath = tokenFilePath
    }

    private func loadClientSecret() throws {
        guard
            let fileURL = URL(
                string: "file://\(FileManager.default.currentDirectoryPath)/\(credentialsFilePath)")
        else {
            throw AppError.clientSecretNotFound
        }
        do {
            let data = try Data(contentsOf: fileURL)
            let secret = try JSONDecoder().decode(ClientSecret.self, from: data)
            if let web = secret.web {
                self.clientSecret = web
            } else if let installed = secret.installed {  // Common for CLI
                self.clientSecret = installed
            } else {
                throw AppError.clientSecretInvalid(
                    "Missing 'web' or 'installed' object in credentials.")
            }
            if !(self.clientSecret?.redirectURIs.contains(redirectURI) ?? false) {
                print(
                    "Warning: The redirectURI '\(redirectURI)' is not listed in your credentials.json. Please ensure it's added to the Google Cloud Console for your OAuth client."
                )
            }
        } catch {
            throw AppError.clientSecretInvalid(
                "Failed to load or parse: \(error.localizedDescription)")
        }
    }

    private func loadToken() throws {
        guard
            let fileURL = URL(
                string: "file://\(FileManager.default.currentDirectoryPath)/\(tokenFilePath)")
        else {
            throw AppError.tokenNotFound  // Or handle as "needs new token"
        }
        guard FileManager.default.fileExists(atPath: fileURL.path) else {
            throw AppError.tokenNotFound
        }
        do {
            let data = try Data(contentsOf: fileURL)
            self.currentToken = try JSONDecoder().decode(OAuthToken.self, from: data)
        } catch {
            throw AppError.authenticationFailed(
                "Failed to load or parse token.json: \(error.localizedDescription)")
        }
    }

    private func saveToken(_ token: OAuthToken) throws {
        guard
            let fileURL = URL(
                string: "file://\(FileManager.default.currentDirectoryPath)/\(tokenFilePath)")
        else {
            throw AppError.ioError("Could not create URL for token.json")
        }
        do {
            let encoder = JSONEncoder()
            encoder.outputFormatting = .prettyPrinted
            // Custom date encoding if needed, or ensure OAuthToken's ExpiryDate is handled well
            let data = try encoder.encode(token)
            try data.write(to: fileURL, options: .atomic)
            print("Saving credential file to: \(fileURL.path)")
        } catch {
            throw AppError.ioError("Failed to save token.json: \(error.localizedDescription)")
        }
    }

    func getAccessToken() async throws -> String {
        if clientSecret == nil {
            try loadClientSecret()
        }

        do {
            try loadToken()
            if let token = currentToken, !token.isExpired() {
                return token.accessToken
            } else if let token = currentToken, let refreshToken = token.refreshToken {
                print("Access token expired, attempting refresh...")
                return try await refreshToken(refreshToken)
            }
        } catch AppError.tokenNotFound {
            // Proceed to get new token
            print("Token not found or invalid. Starting new auth flow.")
        } catch {
            // Other errors during loadToken
            print("Error loading token: \(error). Starting new auth flow.")
        }

        // If no valid token, get a new one
        return try await getTokenFromWeb()
    }

    private func refreshToken(_ refreshToken: String) async throws -> String {
        guard let secret = clientSecret else { throw AppError.clientSecretNotFound }

        var components = URLComponents()
        components.queryItems = [
            URLQueryItem(name: "client_id", value: secret.clientID),
            URLQueryItem(name: "client_secret", value: secret.clientSecret),
            URLQueryItem(name: "refresh_token", value: refreshToken),
            URLQueryItem(name: "grant_type", value: "refresh_token"),
        ]

        var request = URLRequest(url: tokenURL)
        request.httpMethod = "POST"
        request.httpBody = components.query?.data(using: .utf8)
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            let errorBody = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw AppError.authenticationFailed("Token refresh failed: \(errorBody)")
        }

        struct RefreshResponse: Codable {
            let accessToken: String
            let expiresIn: Int
            let tokenType: String
            enum CodingKeys: String, CodingKey {
                case accessToken = "access_token"
                case expiresIn = "expires_in"
                case tokenType = "token_type"
            }
        }
        let refreshResponse = try JSONDecoder().decode(RefreshResponse.self, from: data)

        let newToken = OAuthToken(
            accessToken: refreshResponse.accessToken,
            refreshToken: refreshToken,  // Keep the same refresh token
            tokenType: refreshResponse.tokenType,
            expiresIn: refreshResponse.expiresIn
        )
        self.currentToken = newToken
        try saveToken(newToken)
        return newToken.accessToken
    }

    private func getTokenFromWeb() async throws -> String {
        guard let secret = clientSecret else { throw AppError.clientSecretNotFound }

        var authComponents = URLComponents(string: authURLBase)!
        let state = UUID().uuidString  // For CSRF protection
        authComponents.queryItems = [
            URLQueryItem(name: "client_id", value: secret.clientID),
            URLQueryItem(name: "response_type", value: "code"),
            URLQueryItem(name: "scope", value: gmailScope),
            URLQueryItem(name: "redirect_uri", value: redirectURI),
            URLQueryItem(name: "access_type", value: "offline"),  // To get a refresh token
            URLQueryItem(name: "prompt", value: "consent"),  // Force consent screen for refresh token
            URLQueryItem(name: "state", value: state),
        ]

        guard let url = authComponents.url else {
            throw AppError.authenticationFailed("Could not create auth URL.")
        }

        print(
            "Go to the following link in your browser then type the authorization code or allow the callback:"
        )
        print("\(url.absoluteString)")

        #if canImport(AppKit) && !targetEnvironment(macCatalyst)
            NSWorkspace.shared.open(url)
        #else
            print("Please open the URL manually.")
        #endif

        // **Local HTTP Server for Callback Handling**
        // This part is complex. You'd typically use a library (e.g., Swifter, Vapor, NIOHTTP1Server)
        // to start a server on `redirectURI` (e.g., http://127.0.0.1:8080).
        // The server would:
        // 1. Listen for a GET request to "/oauth2callback".
        // 2. Extract the "code" and "state" query parameters.
        // 3. Verify the "state" parameter.
        // 4. Send the "code" back to this function (e.g., via a Combine PassthroughSubject or an async Stream).
        // 5. Shut down the server.
        //
        // For simplicity, this example will prompt for the code manually.
        // A real implementation should use the local server.

        print("Enter authorization code: ", terminator: "")
        guard let authCode = readLine() else {
            throw AppError.authenticationFailed("No authorization code entered.")
        }

        // Exchange code for token
        var tokenRequestComponents = URLComponents()
        tokenRequestComponents.queryItems = [
            URLQueryItem(name: "code", value: authCode),
            URLQueryItem(name: "client_id", value: secret.clientID),
            URLQueryItem(name: "client_secret", value: secret.clientSecret),
            URLQueryItem(name: "redirect_uri", value: redirectURI),
            URLQueryItem(name: "grant_type", value: "authorization_code"),
        ]

        var request = URLRequest(url: tokenURL)
        request.httpMethod = "POST"
        request.httpBody = tokenRequestComponents.query?.data(using: .utf8)
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            let errorBody = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw AppError.authenticationFailed(
                "Token exchange failed: \(httpResponse.statusCode) - \(errorBody)")
        }

        struct TokenExchangeResponse: Codable {
            let accessToken: String
            let refreshToken: String?
            let expiresIn: Int
            let tokenType: String
            enum CodingKeys: String, CodingKey {
                case accessToken = "access_token"
                case refreshToken = "refresh_token"
                case expiresIn = "expires_in"
                case tokenType = "token_type"
            }
        }

        let tokenResponse = try JSONDecoder().decode(TokenExchangeResponse.self, from: data)
        let newToken = OAuthToken(
            accessToken: tokenResponse.accessToken,
            refreshToken: tokenResponse.refreshToken,
            tokenType: tokenResponse.tokenType,
            expiresIn: tokenResponse.expiresIn
        )
        self.currentToken = newToken
        try saveToken(newToken)
        return newToken.accessToken
    }
}
