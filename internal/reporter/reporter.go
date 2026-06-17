// Package reporter formats the spam count summary for console output.
package reporter

import (
	"fmt"
	"log"
	"sort"
	"time"
)

// PrintSpamSummary prints daily spam counts grouped around the cutoff date,
// followed by the total count. The format matches the original CLI output.
func PrintSpamSummary(spamCounts map[string]int, cutoffDate string) {
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
