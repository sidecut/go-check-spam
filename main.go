package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
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
var maxWorkers = flag.Int("max-workers", 10, "maximum number of concurrent workers")
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

	// Create channels for communication
	msgChan := make(chan *gmail.Message)
	jobChan := make(chan string, 100) // Buffered channel for message IDs

	// Create a WaitGroup to track goroutines
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < *maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for messageId := range jobChan {
				// Check if context is cancelled
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				// delay a random interval between 0 and initialDelay milliseconds to avoid hitting rate limits
				time.Sleep(time.Duration(rand.Intn(*initialDelay)) * time.Millisecond)

				fullMsg, err := backoff.Retry(workerCtx, func() (*gmail.Message, error) {
					// Check if context is cancelled before making API call
					select {
					case <-workerCtx.Done():
						return nil, workerCtx.Err()
					default:
					}

					// Fetch the full message using exponential backoff
					result, err := srv.Users.Messages.Get("me", messageId).Format("minimal").Do()
					if err != nil {
						if *debug {
							log.Printf("Error fetching message %s: %v", messageId, err)
						}
					}
					return result, err

				}, backoff.WithBackOff(backoff.NewExponentialBackOff()))

				// Only try to send if we got a successful result and context isn't cancelled
				if err == nil {
					select {
					case msgChan <- fullMsg:
					case <-workerCtx.Done():
						return
					}
				} else if *debug && !errors.Is(err, context.Canceled) {
					log.Printf("Error fetching message %s: %v", messageId, err)
				}
			}
		}()
	}

	// Calculate the date 'days' ago
	query := "after:" + cutoffDate // Gmail query to filter messages
	fmt.Printf("Gmail query: %s\n", query)
	total := 0

	// Fetch message IDs and send to workers
	go func() {
		defer close(jobChan)
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
						log.Printf("Error fetching messages: %v", err)
					}
				}

				return r, err
			}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
			// Check for errors from the backoff retry
			if err != nil {
				log.Printf("Error fetching message list: %v", err)
				cancel()
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

	// Start a goroutine to close message channel when all workers are done
	go func() {
		wg.Wait()
		close(msgChan)
	}()

	fmt.Print("\r") // erase the in progress count

	// Collect results, taking no more than timeout seconds
	// This is to prevent the program from hanging indefinitely
	timeoutTimer := time.After(time.Duration(*timeout) * time.Second)
	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				msgChan = nil
			} else {
				messages = append(messages, msg)
			}
		case <-timeoutTimer:
			cancel() // Cancel worker context before returning
			return nil, fmt.Errorf("timed out waiting for messages")
		case <-ctx.Done():
			cancel() // Also handle parent context cancellation
			return nil, ctx.Err()
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

	// Validate configuration
	if *days <= 0 {
		log.Fatalf("Days must be positive, got: %d", *days)
	}
	if *timeout <= 0 {
		log.Fatalf("Timeout must be positive, got: %d", *timeout)
	}
	if *maxWorkers <= 0 {
		log.Fatalf("Max workers must be positive, got: %d", *maxWorkers)
	}

	cutoffDate = time.Now().AddDate(0, 0, -*days).Format("2006-01-02")

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received interrupt signal, shutting down gracefully...")
		cancel()
	}()

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
