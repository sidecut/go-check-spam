package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	// Retrieve a token, saves the token, then returns the generated client.
	// Changed to return a TokenSource instead of an http.Client
	ts := getTokenSource(ctx, config)
	return oauth2.NewClient(ctx, ts)
}

// Retrieve a token, saves the token, then returns the generated client.
// Changed to return a TokenSource instead of an http.Client
func getTokenSource(ctx context.Context, config *oauth2.Config) oauth2.TokenSource {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(ctx, config)
		saveToken(tokFile, tok)
	}

	// Create a new TokenSource that can refresh the token
	ts := config.TokenSource(ctx, tok)
	if _, err := ts.Token(); err != nil {
		tok = getTokenFromWeb(ctx, config)
		saveToken(tokFile, tok)
		ts = config.TokenSource(ctx, tok)
	}
	return ts
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(ctx context.Context, config *oauth2.Config) *oauth2.Token {
	// Listen on an ephemeral localhost port and set the redirect URL accordingly.
	authCodeChan := make(chan string, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("Unable to start HTTP server: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://localhost:%d/", port)

	// Use a random state to mitigate CSRF risks.
	state, err := randomState(16)
	if err != nil {
		log.Printf("Warning: failed to generate random state: %v", err)
		state = "state-token"
	}

	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline)
	fmt.Printf("Open the following link in your browser to authorize:\n%v\n", authURL)

	// HTTP handler will accept only requests that match the expected state.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			// Ignore requests with a mismatched state parameter.
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		authCode := r.URL.Query().Get("code")
		if authCode != "" {
			select {
			case authCodeChan <- authCode:
			default:
			}
		}
		fmt.Fprintln(w, "Authorization received. You can close this window.")
	})

	// Start server using the existing listener and return on non-Shutdown errors.
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Try to open the URL in the user's browser.
	if err := openBrowser(authURL); err != nil {
		log.Printf("Error opening browser: %v", err)
		log.Printf("Please manually open the URL in your browser.")
	}

	// Also accept a manually pasted code on stdin as a fallback.
	go func() {
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			select {
			case authCodeChan <- input:
			default:
			}
		}
	}()

	// Wait for the authorization code.
	var authCode string
	select {
	case <-time.After(5 * time.Minute):
		log.Fatal("Timed out waiting for authorization code.")
	case authCode = <-authCodeChan:
	}

	// Gracefully stop the HTTP server now that we have the code.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	tok, err := config.Exchange(ctx, authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	if err != nil {
		return nil, err
	}
	// Remove this check, as expired refresh tokens are ok.
	// if tok.Expiry.Before(time.Now()) {
	// 	return nil, fmt.Errorf("token is expired")
	// }
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(token); err != nil {
		log.Printf("Warning: failed to write token to file: %v", err)
	}
}

// randomState returns a URL-safe base64 encoded random string of n bytes.
func randomState(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// openBrowser tries to open the URL in a browser, preferring the OS's default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}
