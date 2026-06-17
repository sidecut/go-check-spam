// Package spam counts Gmail Spam messages grouped by local date.
package spam

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/sidecut/gocheckspam/internal/config"
	"github.com/sidecut/gocheckspam/internal/gmail"
	"github.com/sidecut/gocheckspam/internal/retry"
	"golang.org/x/sync/errgroup"
)

// Service counts spam messages using a gmail.Client.
type Service struct {
	client gmail.Client
	cfg    *config.Config
}

// NewService returns a Service configured with the provided client and config.
func NewService(client gmail.Client, cfg *config.Config) *Service {
	return &Service{client: client, cfg: cfg}
}

// CountSpamByDate fetches all messages in the SPAM label since cutoffDate and
// returns a map of local date strings to message counts.
func (s *Service) CountSpamByDate(ctx context.Context, cutoffDate string) (map[string]int, error) {
	dailyCounts := make(map[string]int)
	pageToken := ""

	query := "after:" + cutoffDate
	if s.cfg.Debug {
		fmt.Printf("Gmail query: %s\n", query)
	}
	total := 0
	failedFetches := 0

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Timeout)*time.Second)
	defer cancel()

	maxWorkers := s.cfg.Concurrency
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	sem := make(chan struct{}, maxWorkers)

	var mu sync.Mutex
	var eg errgroup.Group

	for {
		var listResp *gmail.ListResponse
		if err := retry.Do(ctx, retry.DefaultConfig(), func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			var err error
			listResp, err = s.client.ListSpamMessages(ctx, query, pageToken)
			if err != nil && s.cfg.Debug {
				log.Printf("Error fetching messages list: %v", err)
			}
			return err
		}); err != nil {
			return nil, fmt.Errorf("error fetching messages: %v", err)
		}

		for _, ref := range listResp.Messages {
			ref := ref
			total++
			fmt.Printf("\r%d", total)

			sem <- struct{}{}
			eg.Go(func() error {
				defer func() { <-sem }()

				if s.cfg.InitialDelay > 0 {
					time.Sleep(time.Duration(rand.Intn(s.cfg.InitialDelay)) * time.Millisecond)
				}

				var fullMsg *gmail.Message
				if err := retry.Do(ctx, retry.DefaultConfig(), func() error {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					var err error
					fullMsg, err = s.client.GetMessage(ctx, ref.ID)
					if err != nil && s.cfg.Debug {
						log.Printf("Error fetching message %s: %v", ref.ID, err)
					}
					return err
				}); err != nil {
					mu.Lock()
					failedFetches++
					mu.Unlock()
					if s.cfg.Debug {
						log.Printf("Failed to fetch message %s: %v", ref.ID, err)
					}
					return nil
				}

				if fullMsg != nil {
					if emailDate := internalDateToDate(fullMsg.InternalDate); emailDate != "" {
						mu.Lock()
						dailyCounts[emailDate]++
						mu.Unlock()
					} else if s.cfg.Debug {
						log.Printf("Warning: Invalid internalDate (%d) for message ID %s", fullMsg.InternalDate, fullMsg.ID)
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

// internalDateToDate converts Gmail InternalDate (ms since epoch) to a
// YYYY-MM-DD date string in the local timezone. Returns empty string for
// invalid timestamps.
func internalDateToDate(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).In(time.Local).Format("2006-01-02")
}
