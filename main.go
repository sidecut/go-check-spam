package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type Config struct {
	Timeout      int
	InitialDelay int
	Days         int
	Debug        bool
	Concurrency  int
	OAuthPort    int
}

func loadConfig() (*Config, error) {
	pflag.Int("timeout", 60, "timeout in seconds")
	pflag.Int("initial-delay", 1000, "max initial delay in milliseconds before starting to fetch messages")
	pflag.Int("days", 30, "number of days to look back")
	pflag.Bool("debug", false, "enable debug output")
	pflag.Int("concurrency", 8, "number of concurrent workers fetching messages")
	pflag.Int("oauth-port", 8080, "port for local OAuth callback server")
	pflag.Parse()

	viper.SetEnvPrefix("GOCHECKSPAM")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("failed to bind flags: %w", err)
	}

	return &Config{
		Timeout:      viper.GetInt("timeout"),
		InitialDelay: viper.GetInt("initial-delay"),
		Days:         viper.GetInt("days"),
		Debug:        viper.GetBool("debug"),
		Concurrency:  viper.GetInt("concurrency"),
		OAuthPort:    viper.GetInt("oauth-port"),
	}, nil
}

func listSpamMessages(ctx context.Context, srv *gmail.Service, cfg *Config, cutoffDate string) (map[string]int, error) {
	dailyCounts := make(map[string]int)
	pageToken := ""

	query := "after:" + cutoffDate
	fmt.Printf("Gmail query: %s\n", query)
	total := 0
	failedFetches := 0

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	maxWorkers := cfg.Concurrency
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
		if err := retryWithBackoff(ctx, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			var err error
			listResp, err = req.Do()
			if err != nil && cfg.Debug {
				log.Printf("Error fetching messages list: %v", err)
			}
			return err
		}); err != nil {
			return nil, fmt.Errorf("error fetching messages: %v", err)
		}

		for _, msg := range listResp.Messages {
			m := msg
			total++
			fmt.Printf("\r%d", total)

			sem <- struct{}{}
			eg.Go(func() error {
				defer func() { <-sem }()

				time.Sleep(time.Duration(rand.Intn(cfg.InitialDelay)) * time.Millisecond)

				var fullMsg *gmail.Message
				if err := retryWithBackoff(ctx, func() error {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					var err error
					fullMsg, err = srv.Users.Messages.Get("me", m.Id).Format("minimal").Do()
					if err != nil && cfg.Debug {
						log.Printf("Error fetching message %s: %v", m.Id, err)
					}
					return err
				}); err != nil {
					mu.Lock()
					failedFetches++
					mu.Unlock()
					if cfg.Debug {
						log.Printf("Failed to fetch message %s: %v", m.Id, err)
					}
					return nil
				}

				if fullMsg != nil {
					if emailDate := internalDateToDate(fullMsg.InternalDate); emailDate != "" {
						mu.Lock()
						dailyCounts[emailDate]++
						mu.Unlock()
					} else if cfg.Debug {
						log.Printf("Warning: Invalid internalDate (%d) for message ID %s", fullMsg.InternalDate, fullMsg.Id)
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

	fmt.Print("\r")

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	if failedFetches > 0 {
		fmt.Printf("Warning: %d of %d message fetches failed\n", failedFetches, total)
	}

	return dailyCounts, nil
}

func printSpamSummary(spamCounts map[string]int, cutoffDate string) {
	cutoff, err := time.Parse("2006-01-02", cutoffDate)
	if err != nil {
		log.Printf("Error parsing cutoff date: %v", err)
		return
	}

	var before, after []string
	for date := range spamCounts {
		dateValue, err := time.Parse("2006-01-02", date)
		if err != nil {
			log.Printf("Error parsing date: %v", err)
			continue
		}
		if dateValue.Before(cutoff) {
			before = append(before, date)
		} else {
			after = append(after, date)
		}
	}
	sort.Strings(before)
	sort.Strings(after)

	total := 0
	printGroup := func(dates []string) {
		for _, date := range dates {
			count := spamCounts[date]
			total += count
			dateValue, _ := time.Parse("2006-01-02", date)
			fmt.Printf("%s %s %d\n", dateValue.Format("Mon"), date, count)
		}
	}

	printGroup(before)
	if len(before) > 0 && len(after) > 0 {
		fmt.Println()
	}
	printGroup(after)

	fmt.Printf("Total: %d\n", total)
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Unable to load configuration: %v", err)
	}
	cutoffDate := time.Now().AddDate(0, 0, -cfg.Days).Format("2006-01-02")

	ctx := context.Background()
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	config.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.OAuthPort)
	client := getClient(ctx, config, cfg.OAuthPort)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	spamCounts, err := listSpamMessages(ctx, srv, cfg, cutoffDate)
	if err != nil {
		log.Fatalf("Unable to list spam messages: %v", err)
	}

	if len(spamCounts) == 0 {
		fmt.Println("No spam messages found.")
		return
	}

	fmt.Printf("Spam email counts for the past %v days (based on internalDate):\n", cfg.Days)
	printSpamSummary(spamCounts, cutoffDate)
}

// retryWithBackoff retries the provided operation with exponential backoff
// until it succeeds, the context is cancelled, or a non-retryable error occurs.
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
			if isNonRetryable(err) {
				return err
			}
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

// isNonRetryable checks whether an error is a Google API error with a
// non-retryable HTTP status code (4xx except 429).
func isNonRetryable(err error) bool {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		return code != 429 && code >= 400 && code < 500
	}
	return false
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
