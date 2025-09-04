package main

import (
	"testing"
	"time"
)

func TestInternalDateToDate(t *testing.T) {
	// 2020-01-02 03:04:05 UTC in milliseconds
	ts := int64(1577936645000)
	got := internalDateToDate(ts)
	// convert to local date for expected
	expected := time.UnixMilli(ts).In(time.Local).Format("2006-01-02")
	if got != expected {
		t.Fatalf("expected %s got %s", expected, got)
	}

	if internalDateToDate(0) != "" {
		t.Fatalf("expected empty string for zero timestamp")
	}
}
