package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var timeout = flag.Int("timeout", 60, "timeout in seconds")
var days = flag.Int("days", 31, "number of days to look back")

func init() {
	flag.Parse()
}

func getSpamCounts(srv *gmail.Service) (map[string]int, error) {
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

		dailyCounts[emailDate]++
	}

	return dailyCounts, nil
}

func listSpamMessages(srv *gmail.Service) ([]*gmail.Message, error) {
	var messages []*gmail.Message
	pageToken := ""

	// Create a channel to receive messages
	msgChan := make(chan *gmail.Message)
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
	}()

	// Calculate the date 'days' ago
	cutoff := time.Now().AddDate(0, 0, -*days).Format("2006/01/02")
	query := "after:" + cutoff // Gmail query to filter messages
	total := 0

	for {
		req := srv.Users.Messages.List("me").LabelIds("SPAM").Q(query)
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		var r *gmail.ListMessagesResponse
		fib := NewFib()
		for {
			var err error
			r, err = req.Do()
			if err == nil {
				break
			}
			time.Sleep(time.Duration(fib.next()) * time.Second)
		}

		// Process messages in parallel
		for _, msg := range r.Messages {
			wg.Add(1)
			go func(messageId string) {
				defer wg.Done()
				fib := NewFib()
				for {
					fullMsg, err := srv.Users.Messages.Get("me", messageId).Format("minimal").Do()
					if err == nil {
						msgChan <- fullMsg
						break
					}
					time.Sleep(time.Duration(fib.next()) * time.Second)
				}
			}(msg.Id)
			total++
			fmt.Printf("\r%d", total)
		}

		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}

	fmt.Print("\r") // erase the in progress count
	wg.Done()

	// Collect results, taking no more than 60 seconds
	// This is to prevent the program from hanging indefinitely
	timeout := time.After(time.Duration(*timeout) * time.Second)
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

	fmt.Printf("Spam email counts for the past %v days (based on internalDate):\n", *days)
	var dates []string
	for date := range spamCounts {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	total := 0
	for _, date := range dates {
		count := spamCounts[date]
		total += count
		dateValue, err := time.Parse("2006-01-02", date)
		if err != nil {
			fmt.Printf("Error parsing date: %v\n", err)
			continue
		}
		dayOfWeek := dateValue.Format("Mon")
		fmt.Printf("%s, %s: %d\n", dayOfWeek, date, count)
	}
	fmt.Printf("Total: %d\n", total)
}
