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

func getClient(ctx context.Context, config *oauth2.Config) (*http.Client, error) {
	// Retrieve a token source (which may prompt the user), then return a configured http.Client.
	ts, err := getTokenSource(ctx, config)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, ts), nil
}

// Retrieve a token, saves the token, then returns the generated client.
// Changed to return a TokenSource instead of an http.Client
func getTokenSource(ctx context.Context, config *oauth2.Config) (oauth2.TokenSource, error) {
	tokFile := os.Getenv("TOKEN_FILE")
	if tokFile == "" {
		tokFile = "token.json"
	}

	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok, err = getTokenFromWeb(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to obtain token from web: %w", err)
		}
		if err := saveToken(tokFile, tok); err != nil {
			log.Printf("Warning: failed to save token to %s: %v", tokFile, err)
		}
	}

	// Create a TokenSource that can refresh the token
	ts := config.TokenSource(ctx, tok)
	if _, err := ts.Token(); err != nil {
		tok, err = getTokenFromWeb(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token from web: %w", err)
		}
		if err := saveToken(tokFile, tok); err != nil {
			log.Printf("Warning: failed to save token to %s: %v", tokFile, err)
		}
		ts = config.TokenSource(ctx, tok)
	}
	return ts, nil
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Listen on an ephemeral localhost port and set the redirect URL accordingly.
	authCodeChan := make(chan string, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("unable to start HTTP listener: %w", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	// Copy config to avoid mutating caller's config
	cfg := *config
	cfg.RedirectURL = fmt.Sprintf("http://localhost:%d/", port)

	// Use a random state to mitigate CSRF risks.
	state, err := randomState(16)
	if err != nil {
		log.Printf("Warning: failed to generate random state: %v", err)
		state = "state-token"
	}
	// Request offline access and force consent to ensure Google returns a refresh token
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
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
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Try to open the URL in the user's browser.
	if err := openBrowser(authURL); err != nil {
		log.Printf("Error opening browser: %v", err)
		log.Printf("Please manually open the URL in your browser.")
	}

	// Also accept a manually pasted code on stdin as a fallback. Use a cancellable
	// context so the goroutine stops when the auth flow completes.
	stdinCtx, stdinCancel := context.WithCancel(context.Background())
	defer stdinCancel()
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			select {
			case <-stdinCtx.Done():
				return
			default:
			}
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input != "" {
				select {
				case authCodeChan <- input:
				default:
				}
			}
		}
	}()

	// Wait for the authorization code.
	var authCode string
	select {
	case <-time.After(5 * time.Minute):
		// Gracefully stop the HTTP server and return an error
		stdinCancel()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error after timeout: %v", err)
		}
		return nil, fmt.Errorf("timed out waiting for authorization code")
	case authCode = <-authCodeChan:
		// stop stdin goroutine as we got the code
		stdinCancel()
	}

	// Gracefully stop the HTTP server now that we have the code.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	tok, err := cfg.Exchange(ctx, authCode)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %w", err)
	}
	return tok, nil
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
func saveToken(path string, token *oauth2.Token) error {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("unable to open token file for writing: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(token); err != nil {
		return fmt.Errorf("failed to encode token to file: %w", err)
	}
	return nil
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
		// Use empty title to handle URLs with special characters
		args = []string{"/c", "start", "", url}
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
