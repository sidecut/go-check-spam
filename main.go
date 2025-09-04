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

	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var timeout = flag.Int("timeout", 60, "timeout in seconds")
var initialDelay = flag.Int("initial-delay", 1000, "max initial delay in milliseconds before starting to fetch messages")
var days = flag.Int("days", 30, "number of days to look back")
var debug = flag.Bool("debug", false, "enable debug output")
var concurrency = flag.Int("concurrency", 8, "number of concurrent workers fetching messages")
var oauthPort = flag.Int("oauth-port", 8080, "port for local OAuth callback server")
var cutoffDate string

func getSpamCounts(ctx context.Context, srv *gmail.Service) (map[string]int, error) {
	// listSpamMessages now performs counting and returns the map directly.
	dailyCounts, err := listSpamMessages(ctx, srv)
	if err != nil {
		return nil, fmt.Errorf("unable to list spam messages: %v", err)
	}

	if len(dailyCounts) == 0 {
		fmt.Println("No spam messages found.")
	}

	return dailyCounts, nil
}

func listSpamMessages(ctx context.Context, srv *gmail.Service) (map[string]int, error) {
	dailyCounts := make(map[string]int)
	pageToken := ""

	// We'll collect full messages into `messages` but fetch them using a
	// bounded worker pool to avoid launching an unbounded number of
	// goroutines. Use errgroup for easier error handling.

	// Calculate the date 'days' ago
	query := "after:" + cutoffDate // Gmail query to filter messages
	fmt.Printf("Gmail query: %s\n", query)
	total := 0

	// Use a cancellable context with timeout so the whole listing/fetching
	// process respects the -timeout flag.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*timeout)*time.Second)
	defer cancel()

	// Bounded concurrency for fetching full messages
	maxWorkers := *concurrency
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	sem := make(chan struct{}, maxWorkers)

	var mu sync.Mutex
	var eg errgroup.Group

	for {
		req := srv.Users.Messages.List("me").LabelIds("SPAM").Q(query)
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		var listResp *gmail.ListMessagesResponse
		// Wrap the request with a context check so we exit quickly if the
		// parent context is cancelled.
		if err := retryWithBackoff(ctx, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			var err error
			listResp, err = req.Do()
			if err != nil && *debug {
				log.Printf("Error fetching messages list: %v", err)
			}
			return err
		}); err != nil {
			return nil, fmt.Errorf("error fetching messages: %v", err)
		}

		// Process messages with bounded concurrency
		for _, msg := range listResp.Messages {
			m := msg
			total++
			fmt.Printf("\r%d", total)

			sem <- struct{}{}
			eg.Go(func() error {
				defer func() { <-sem }()

				// delay a random interval between 0 and initialDelay milliseconds to avoid hitting rate limits
				time.Sleep(time.Duration(rand.Intn(*initialDelay)) * time.Millisecond)

				var fullMsg *gmail.Message
				if err := retryWithBackoff(ctx, func() error {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					var err error
					fullMsg, err = srv.Users.Messages.Get("me", m.Id).Format("minimal").Do()
					if err != nil && *debug {
						log.Printf("Error fetching message %s: %v", m.Id, err)
					}
					return err
				}); err != nil {
					if *debug {
						log.Printf("Failed to fetch message %s: %v", m.Id, err)
					}
					return nil // non-fatal; continue with other messages
				}

				if fullMsg != nil {
					// internalDate is milliseconds since epoch
					internalDateMs := fullMsg.InternalDate
					if internalDateMs > 0 {
						emailTimeLocal := time.UnixMilli(internalDateMs).In(time.Local)
						emailDate := emailTimeLocal.Format("2006-01-02")
						mu.Lock()
						dailyCounts[emailDate]++
						mu.Unlock()
					} else if *debug {
						log.Printf("Warning: Invalid internalDate (%d) for message ID %s", internalDateMs, fullMsg.Id)
					}
				}
				return nil
			})
		}

		pageToken = listResp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	fmt.Print("\r") // erase the in progress count

	// Wait for all workers to finish (or context timeout)
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return dailyCounts, nil
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

	// The global random number generator is automatically seeded in Go 1.20+.

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

// retryWithBackoff retries the provided operation with exponential backoff
// until it succeeds or the context is cancelled.
func retryWithBackoff(ctx context.Context, op func() error) error {
	wait := 300 * time.Millisecond
	maxAttempts := 8
	for i := range maxAttempts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := op(); err == nil {
			return nil
		} else {
			if i == maxAttempts-1 {
				return err
			}
			jitter := time.Duration(rand.Intn(200)) * time.Millisecond
			time.Sleep(wait + jitter)
			wait *= 2
			if wait > 10*time.Second {
				wait = 10 * time.Second
			}
		}
	}
	return fmt.Errorf("retry attempts exhausted")
}

// internalDateToDate converts gmail InternalDate (ms since epoch) to a
// YYYY-MM-DD date string in the local timezone. Returns empty string for
// invalid timestamps.
func internalDateToDate(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).In(time.Local).Format("2006-01-02")
}
