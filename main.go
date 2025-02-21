package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
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

func getSpamCounts(srv *gmail.Service) (map[string]int, error) {
	today := time.Now()
	dailyCounts := make(map[string]int)

	// Get all messages in the SPAM folder
	messages, err := listSpamMessages(srv)
	if err != nil {
		return nil, fmt.Errorf("unable to list spam messages: %v", err)
	}

	if len(messages) == 0 {
		fmt.Println("No spam messages found.")
		return dailyCounts, nil
	}

	// Process each message to extract internalDate
	for _, m := range messages {
		// internalDate is returned as milliseconds since epoch
		internalDateMs := m.InternalDate
		// Convert to seconds
		internalDateSec := internalDateMs / 1000
		// Create time object
		emailTime := time.Unix(internalDateSec, 0)
		emailDate := emailTime.Format("2006-01-02") // Format as YYYY-MM-DD

		// Check if the email is within the past 31 days
		daysAgo := today.Sub(emailTime).Hours() / 24
		if daysAgo <= 31 {
			dailyCounts[emailDate]++
		}
	}

	return dailyCounts, nil
}

func listSpamMessages(srv *gmail.Service) ([]*gmail.Message, error) {
	var messages []*gmail.Message
	pageToken := ""
	batchSize := 500 // Define the batch size

	// Create a channel to receive messages
	msgChan := make(chan *gmail.Message, batchSize) // Buffer the channel
	// Create a channel to receive errors
	errChan := make(chan error)
	// Create a WaitGroup to track goroutines
	var wg sync.WaitGroup

	// Start a goroutine to close channels when all workers are done
	wg.Add(1)
	go func() {
		wg.Wait()
		close(msgChan)
		close(errChan)
		print("All workers are done. Closing channels.\n")
	}()

	for {
		req := srv.Users.Messages.List("me").LabelIds("SPAM")
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		req.MaxResults(int64(batchSize)) // Set the batch size
		r, err := req.Do()
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve messages: %v", err)
		}

		// Process messages in parallel
		for _, msg := range r.Messages {
			wg.Add(1)
			go func(messageId string) {
				defer wg.Done()
				fullMsg, err := srv.Users.Messages.Get("me", messageId).Format("minimal").Do()
				if err != nil {
					errChan <- fmt.Errorf("unable to retrieve message details: %v", err)
					return
				}
				msgChan <- fullMsg
			}(msg.Id)
			fmt.Print(".")
		}

		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
		fmt.Print(".")
	}

	fmt.Println("")
	print("All messages have been retrieved.\n")
	wg.Done()

	// Collect results, taking no more than 60 seconds
	// This is to prevent the program from hanging indefinitely
	timeout := time.After(60 * time.Second)
	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				msgChan = nil
			} else {
				messages = append(messages, msg)
			}
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			} else {
				return nil, err
			}
		case <-timeout:
			return nil, fmt.Errorf("timed out waiting for messages")
		}

		if msgChan == nil && errChan == nil {
			break
		}
	}

	// Check for any errors
	for err := range errChan {
		if err != nil {
			return nil, err
		}
	}

	return messages, nil
}

func main() {
	ctx := context.Background()
	b, err := os.ReadFile("credentials.json") // Download from Google Cloud Console
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	spamCounts, err := getSpamCounts(srv)
	if err != nil {
		log.Fatalf("Error getting spam counts: %v", err)
	}

	fmt.Println("Spam email counts for the past 31 days (based on internalDate):")
	for date, count := range spamCounts {
		fmt.Printf("%s: %d\n", date, count)
	}
}
