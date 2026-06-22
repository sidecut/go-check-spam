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
	gmailapi "google.golang.org/api/gmail/v1"
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
		log.Fatalf("Unable to read credentials.json: %v", err)
	}

	oauthCfg, err := google.ConfigFromJSON(b, gmailapi.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse credentials.json: %v", err)
	}
	oauthCfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.OAuthPort)

	httpClient := auth.NewClient(ctx, oauthCfg, cfg.OAuthPort)

	gmailClient, err := gmail.NewServiceClient(ctx, httpClient)
	if err != nil {
		log.Fatalf("Unable to create Gmail client: %v", err)
	}

	svc := spam.NewService(gmailClient, cfg)
	counts, err := svc.CountSpamByDate(ctx, cutoffDate)
	if err != nil {
		log.Fatalf("Error counting spam: %v", err)
	}

	reporter.PrintSpamSummary(counts, cutoffDate)
}
