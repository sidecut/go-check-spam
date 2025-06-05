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

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var timeout = flag.Int("timeout", 60, "timeout in seconds")
var days = flag.Int("days", 30, "number of days to look back")
var debug = flag.Bool("debug", false, "enable debug output")
var cutoffDate string

func getSpamCounts(ctx context.Context, srv *gmail.Service) (map[string]int, error) {
	dailyCounts := make(map[string]int)

	// Get all messages in the SPAM folder
	messages, err := listSpamMessages(ctx, srv)
	if err != nil {
		return nil, fmt.Errorf("unable to list spam messages: %v", err)
	}

	if len(messages) == 0 {
		fmt.Println("No spam messages found.")
		return dailyCounts, nil
	}

	// Process each message to extract internalDate
	for _, m := range messages {
		// internalDate is returned as milliseconds since epoch (assumed to be UTC/GMT)
		internalDateMs := m.InternalDate

		// Safety check for invalid dates
		if internalDateMs <= 0 {
			if *debug {
				fmt.Printf("Warning: Invalid internalDate (%d) for message ID %s\n", internalDateMs, m.Id)
			}
			continue
		}

		// Create a time.Time object from the UTC epoch milliseconds.
		// time.UnixMilli converts the UTC epoch milliseconds to a time.Time object
		// representing that instant in the local system timezone.
		emailTimeLocal := time.UnixMilli(internalDateMs)

		// Format the local time to get the local date string in YYYY-MM-DD format
		emailDate := emailTimeLocal.Format("2006-01-02")

		dailyCounts[emailDate]++
	}

	return dailyCounts, nil
}

func listSpamMessages(ctx context.Context, srv *gmail.Service) ([]*gmail.Message, error) {
	var messages []*gmail.Message
	pageToken := ""

	// Create a channel to receive messages
	msgChan := make(chan *gmail.Message)
	// Create a WaitGroup to track goroutines
	var wg sync.WaitGroup

	// Start a goroutine to close channels when all workers are done
	wg.Add(1)
	go func() {
		wg.Wait()
		close(msgChan)
	}()

	// Calculate the date 'days' ago
	query := "after:" + cutoffDate // Gmail query to filter messages
	fmt.Printf("Gmail query: %s\n", query)
	total := 0

	for {
		req := srv.Users.Messages.List("me").LabelIds("SPAM").Q(query)
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		r, err := backoff.Retry(ctx, func() (*gmail.ListMessagesResponse, error) {
			// Use exponential backoff to handle rate limiting and transient errors
			r, err := req.Do()

			if err != nil {
				if *debug {
					fmt.Printf("Error fetching messages: %v\n", err)
				}
			}

			return r, err
		}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
		// Check for errors from the backoff retry
		if err != nil {
			return nil, fmt.Errorf("error fetching messages: %v", err)
		}

		// Process messages in parallel
		for _, msg := range r.Messages {
			wg.Add(1)
			go func(messageId string) {
				defer wg.Done()

				// fib := NewFib()
				// for {
				fullMsg, err := backoff.Retry(ctx, func() (*gmail.Message, error) {
					// Fetch the full message using exponential backoff
					result, err := srv.Users.Messages.Get("me", messageId).Format("minimal").Do()
					if err != nil {
						if *debug {
							fmt.Printf("Error fetching message %s: %v\n", messageId, err)
						}
					}
					return result, err

				}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
				if err == nil {
					msgChan <- fullMsg
				} else if *debug {
					fmt.Printf("Error fetching message %s: %v\n", messageId, err)
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
		case <-timeout:
			return nil, fmt.Errorf("timed out waiting for messages")
		}

		if msgChan == nil {
			break
		}
	}

	return messages, nil
}

type outputStates int

const (
	FirstLine outputStates = iota
	BeforeDate
	OnOrAfterDate
)

func printSpamSummary(spamCounts map[string]int) {
	var dates []string
	for date := range spamCounts {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	total := 0
	outputState := FirstLine
	for _, date := range dates {
		if date < cutoffDate {
			outputState = BeforeDate
			// log.Default().Printf("Switching to BEFORE_DATE for date: %s\n", date)
		} else {
			if outputState == BeforeDate {
				// Print a blank line to separate sections
				fmt.Println()
			}
			outputState = OnOrAfterDate
		}

		count := spamCounts[date]
		total += count
		dateValue, err := time.Parse("2006-01-02", date)
		if err != nil {
			fmt.Printf("Error parsing date: %v\n", err)
			continue
		}
		dayOfWeek := dateValue.Format("Mon")
		fmt.Printf("%s %s %d\n", dayOfWeek, date, count)
	}
	fmt.Printf("Total: %d\n", total)
}

func main() {
	flag.Parse()
	cutoffDate = time.Now().AddDate(0, 0, -*days).Format("2006-01-02")

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
	client := getClient(ctx, config)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	spamCounts, err := getSpamCounts(ctx, srv)
	if err != nil {
		log.Fatalf("Error getting spam counts: %v", err)
	}

	fmt.Printf("Spam email counts for the past %v days (based on internalDate):\n", *days)
	printSpamSummary(spamCounts)
}
