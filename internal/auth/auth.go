// Package auth handles OAuth2 token retrieval and refresh for Gmail access.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
)

// NewClient retrieves or refreshes an OAuth token and returns an HTTP client
// authorized for Gmail access. It expects a token file named token.json in the
// working directory and writes the token back after a fresh authorization.
func NewClient(ctx context.Context, config *oauth2.Config, oauthPort int) *http.Client {
	ts := getTokenSource(ctx, config, oauthPort)
	return oauth2.NewClient(ctx, ts)
}

func getTokenSource(ctx context.Context, config *oauth2.Config, oauthPort int) oauth2.TokenSource {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(ctx, config, oauthPort)
		saveToken(tokFile, tok)
	}

	return config.TokenSource(ctx, tok)
}

// getTokenFromWeb requests a new token from the user via browser or terminal.
func getTokenFromWeb(ctx context.Context, config *oauth2.Config, oauthPort int) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	authCodeChan := make(chan string, 2) // buffered so both senders can exit even if only one is read

	mux := http.NewServeMux()
	addr := fmt.Sprintf("127.0.0.1:%d", oauthPort)
	srv := &http.Server{Addr: addr, Handler: mux}
	ready := make(chan struct{})

	go func() {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			authCode := r.URL.Query().Get("code")
			fmt.Println("") // Print a newline because there's a dangling "Enter authorization code: " in the terminal
			fmt.Printf("Received authorization code: %s\n", authCode)
			fmt.Fprint(w, "Authorization received. You can close this window.")
			go func() { authCodeChan <- authCode }()

			go func() {
				_ = srv.Shutdown(context.Background())
			}()
		})

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("Unable to start HTTP server: %v", err)
		}
		close(ready)

		if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Unable to start HTTP server: %v", err)
		}
	}()

	<-ready

	go func() {
		fmt.Print("Enter authorization code: ")
		var authCode string
		if _, err := fmt.Scan(&authCode); err != nil {
			log.Fatalf("Unable to scan authorization code: %v", err)
		}
		authCodeChan <- authCode
	}()

	if err := openBrowser(authURL); err != nil {
		log.Printf("Error opening browser: %v", err)
		log.Printf("Please manually open the URL in your browser.")
	}

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

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tok := &oauth2.Token{}
	if err := json.NewDecoder(f).Decode(tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(token)
}

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
