package main_test

import (
	"fmt"
	"testing"
	"time"
	_ "time/tzdata"
)

// test converting UTC to local time
func TestUTCToLocalConversion(t *testing.T) {
	// UTC time
	utcTime := time.Date(2023, 10, 2, 3, 0, 0, 0, time.UTC)

	// Convert to New York local time
	// Note: The local time zone is determined by the system's time zone settings.
	// If you want to test with a specific time zone, you can use time.LoadLocation
	// to load a specific time zone.
	// For example, to load the "America/New_York" time zone:
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	localTime := utcTime.In(loc)

	// Print both times
	fmt.Println("UTC Time:  ", utcTime)
	fmt.Println("Local Time:", localTime)
	fmt.Println("Local Time (formatted):", localTime.Format("2006-01-02"))

	// Make sure that the date part of the local time is 10/1/2023
	if localTime.Year() != 2023 || localTime.Month() != 10 || localTime.Day() != 1 {
		t.Errorf("expected local time to be 2023-10-01, got %s", localTime.Format("2006-01-02"))
	}

	// Output:
	// UTC Time: 2023-10-01 12:00:00 +0000 UTC
	// Local Time: 2023-10-01 08:00:00 -0400 EDT
}
