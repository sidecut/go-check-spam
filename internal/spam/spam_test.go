package spam

import (
	"context"
	"testing"
	"time"

	"github.com/sidecut/gocheckspam/internal/config"
	"github.com/sidecut/gocheckspam/internal/gmail"
	"github.com/sidecut/gocheckspam/internal/gmail/fake"
)

func TestInternalDateToDate(t *testing.T) {
	ts := int64(1577936645000)
	got := internalDateToDate(ts)
	expected := time.UnixMilli(ts).In(time.Local).Format("2006-01-02")
	if got != expected {
		t.Fatalf("expected %s got %s", expected, got)
	}

	if internalDateToDate(0) != "" {
		t.Fatalf("expected empty string for zero timestamp")
	}
}

func TestCountSpamByDate_Empty(t *testing.T) {
	client := fake.NewClient()
	cfg := &config.Config{Timeout: 60, Concurrency: 1, InitialDelay: 0}

	counts, err := NewService(client, cfg).CountSpamByDate(context.Background(), "2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty counts, got %v", counts)
	}
}

func TestCountSpamByDate_AggregatesByDate(t *testing.T) {
	client := fake.NewClient().
		WithPages([]fake.Page{
			{
				Messages: []gmail.MessageRef{
					{ID: "a"},
					{ID: "b"},
					{ID: "c"},
				},
			},
		}).
		WithMessage("a", &gmail.Message{ID: "a", InternalDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli()}).
		WithMessage("b", &gmail.Message{ID: "b", InternalDate: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC).UnixMilli()}).
		WithMessage("c", &gmail.Message{ID: "c", InternalDate: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC).UnixMilli()})

	cfg := &config.Config{Timeout: 60, Concurrency: 3, InitialDelay: 0}
	counts, err := NewService(client, cfg).CountSpamByDate(context.Background(), "2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	localDate := func(ts int64) string {
		return time.UnixMilli(ts).In(time.Local).Format("2006-01-02")
	}

	expected := map[string]int{}
	for _, ts := range []int64{
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli(),
		time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC).UnixMilli(),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC).UnixMilli(),
	} {
		expected[localDate(ts)]++
	}

	if len(counts) != len(expected) {
		t.Fatalf("expected %d dates, got %v", len(expected), counts)
	}

	for date, want := range expected {
		if got := counts[date]; got != want {
			t.Errorf("expected %d for %s, got %d", want, date, got)
		}
	}
}

func TestCountSpamByDate_Pagination(t *testing.T) {
	client := fake.NewClient().
		WithPages([]fake.Page{
			{Messages: []gmail.MessageRef{{ID: "a"}}, NextPageToken: "next"},
			{Messages: []gmail.MessageRef{{ID: "b"}}},
		}).
		WithMessage("a", &gmail.Message{ID: "a", InternalDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli()}).
		WithMessage("b", &gmail.Message{ID: "b", InternalDate: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC).UnixMilli()})

	cfg := &config.Config{Timeout: 60, Concurrency: 1, InitialDelay: 0}
	counts, err := NewService(client, cfg).CountSpamByDate(context.Background(), "2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.ListCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 list calls, got %d", len(calls))
	}
	if calls[1].PageToken != "next" {
		t.Errorf("expected second call page token 'next', got %q", calls[1].PageToken)
	}

	if len(counts) != 2 {
		t.Fatalf("expected 2 dates, got %v", counts)
	}
}

func TestCountSpamByDate_FetchFailureIgnored(t *testing.T) {
	client := fake.NewClient().
		WithPages([]fake.Page{
			{Messages: []gmail.MessageRef{{ID: "a"}, {ID: "b"}}},
		}).
		WithMessage("a", &gmail.Message{ID: "a", InternalDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli()})

	cfg := &config.Config{Timeout: 60, Concurrency: 1, InitialDelay: 0}
	counts, err := NewService(client, cfg).CountSpamByDate(context.Background(), "2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(counts) != 1 {
		t.Fatalf("expected 1 date, got %v", counts)
	}
}
