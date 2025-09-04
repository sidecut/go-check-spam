package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
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
	ts := config.TokenSource(context.Background(), tok)
	return ts
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(ctx context.Context, config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	authCodeChan := make(chan string, 1)
	mux := http.NewServeMux()
	srv := &http.Server{Addr: ":80", Handler: mux}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// We get several requests to the root URL, so we need to filter out the favicon request.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
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

	go func() {
		// Start a web server to handle the callback and exchange the code.
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Unable to start HTTP server: %v", err)
		}
	}()

	// Open the URL in the user's browser.
	err := openBrowser(authURL)
	if err != nil {
		log.Printf("Error opening browser: %v", err)
		log.Printf("Please manually open the URL in your browser.")
	}

	// Wait for the authorization code to be received from either the terminal *or* the web server.
	// This is done to handle the case where the user manually enters the code in the terminal.
	// This select statement will block until one of the two cases occurs.

	var authCode string
	select {
	case <-time.After(60 * time.Second):
		log.Fatal("Timed out waiting for authorization code.")
	case authCode = <-authCodeChan:
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
	json.NewEncoder(f).Encode(token)
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
