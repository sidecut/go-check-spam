package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
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
var initialDelay = flag.Int("initial-delay", 1000, "max initial delay in milliseconds before starting to fetch messages")
var days = flag.Int("days", 30, "number of days to look back")
var debug = flag.Bool("debug", false, "enable debug output")
var workers = flag.Int("workers", 10, "number of worker goroutines to use for fetching messages")
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
				log.Printf("Warning: Invalid internalDate (%d) for message ID %s", internalDateMs, m.Id)
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
	// Create cancellable context for workers
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var messages []*gmail.Message
	pageToken := ""

	// Create channels for the worker pool
	msgChan := make(chan *gmail.Message, 100) // Buffered to prevent worker blocking
	jobChan := make(chan string, 100)         // Buffered channel for message IDs
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for messageId := range jobChan {
				// Delay to avoid hitting rate limits
				time.Sleep(time.Duration(rand.Intn(*initialDelay)) * time.Millisecond)

				fullMsg, err := backoff.Retry(workerCtx, func() (*gmail.Message, error) {
					// Fetch the full message using exponential backoff
					result, err := srv.Users.Messages.Get("me", messageId).Format("minimal").Do()
					if err != nil {
						if *debug {
							log.Printf("Worker %d: Error fetching message %s: %v", workerID, messageId, err)
						}
					}
					return result, err
				}, backoff.WithBackOff(backoff.NewExponentialBackOff()))

				if err == nil {
					select {
					case msgChan <- fullMsg:
					case <-workerCtx.Done():
						return
					}
				} else if *debug {
					log.Printf("Worker %d: Error fetching message %s: %v", workerID, messageId, err)
				}
			}
		}(i)
	}

	// Start a goroutine to close msgChan when all workers are done
	go func() {
		wg.Wait()
		close(msgChan)
	}()

	// Calculate the date 'days' ago
	query := "after:" + cutoffDate // Gmail query to filter messages
	fmt.Printf("Gmail query: %s\n", query)
	total := 0

	// Fetch message IDs and send them to the worker pool
	go func() {
		defer close(jobChan)

		for {
			// Check if context is cancelled before making API calls
			select {
			case <-workerCtx.Done():
				return
			default:
			}

			req := srv.Users.Messages.List("me").LabelIds("SPAM").Q(query)
			if pageToken != "" {
				req = req.PageToken(pageToken)
			}

			r, err := backoff.Retry(workerCtx, func() (*gmail.ListMessagesResponse, error) {
				// Use exponential backoff to handle rate limiting and transient errors
				r, err := req.Do()

				if err != nil {
					if *debug {
						log.Printf("Error fetching messages: %v", err)
					}
				}

				return r, err
			}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
			// Check for errors from the backoff retry
			if err != nil {
				log.Printf("Error fetching message list: %v", err)
				return
			}

			// Send message IDs to worker pool
			for _, msg := range r.Messages {
				select {
				case jobChan <- msg.Id:
					total++
					fmt.Printf("\r%d", total)
				case <-workerCtx.Done():
					return
				}
			}

			pageToken = r.NextPageToken
			if pageToken == "" {
				break
			}
		}
	}()

	fmt.Print("\r") // erase the in progress count

	// Collect results, taking no more than specified timeout seconds
	timeout := time.After(time.Duration(*timeout) * time.Second)
	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				return messages, nil
			}
			messages = append(messages, msg)
		case <-timeout:
			cancel()
			return nil, fmt.Errorf("timed out waiting for messages")
		case <-ctx.Done():
			cancel()
			return nil, ctx.Err()
		}
	}
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
			log.Printf("Error parsing date: %v", err)
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
