// gocheckspam counts Gmail Spam messages grouped by local date.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/sidecut/gocheckspam/internal/auth"
	"github.com/sidecut/gocheckspam/internal/config"
	"github.com/sidecut/gocheckspam/internal/gmail"
	"github.com/sidecut/gocheckspam/internal/reporter"
	"github.com/sidecut/gocheckspam/internal/spam"
	"golang.org/x/oauth2/google"
	googlemail "google.golang.org/api/gmail/v1"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Unable to load configuration: %v", err)
	}

	cutoffDate := time.Now().AddDate(0, 0, -cfg.Days).Format("2006-01-02")

	ctx := context.Background()
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	oauthCfg, err := google.ConfigFromJSON(b, googlemail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	oauthCfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.OAuthPort)

	client := auth.NewClient(ctx, oauthCfg, cfg.OAuthPort)
	gmailClient, err := gmail.NewServiceClient(ctx, client)
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	spamCounts, err := spam.NewService(gmailClient, cfg).CountSpamByDate(ctx, cutoffDate)
	if err != nil {
		log.Fatalf("Unable to list spam messages: %v", err)
	}

	if len(spamCounts) == 0 {
		fmt.Println("No spam messages found.")
		return
	}

	fmt.Printf("Spam email counts for the past %v days (based on internalDate):\n", cfg.Days)
	reporter.PrintSpamSummary(spamCounts, cutoffDate)
}
