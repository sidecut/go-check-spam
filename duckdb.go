package main

import (
	"database/sql"
	"fmt"
	"sort"

	_ "github.com/duckdb/duckdb-go/v2"
)

func syncSpamCounts(dbPath string, spamCounts map[string]int, cutoffDate string) error {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS spam_by_date (
			date TEXT PRIMARY KEY,
			spam_count INTEGER NOT NULL,
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	selectStmt, err := tx.Prepare(`SELECT spam_count FROM spam_by_date WHERE date = ?`)
	if err != nil {
		return fmt.Errorf("prepare select: %w", err)
	}
	defer selectStmt.Close()

	insertStmt, err := tx.Prepare(`INSERT INTO spam_by_date (date, spam_count) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	updateStmt, err := tx.Prepare(`UPDATE spam_by_date SET spam_count = ?, updated_at = NOW() WHERE date = ?`)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer updateStmt.Close()

	dates := make([]string, 0, len(spamCounts))
	for date := range spamCounts {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	for _, date := range dates {
		if date < cutoffDate {
			continue
		}

		newCount := spamCounts[date]

		var existingCount int
		err := selectStmt.QueryRow(date).Scan(&existingCount)
		switch {
		case err == sql.ErrNoRows:
			if _, err := insertStmt.Exec(date, newCount); err != nil {
				return fmt.Errorf("insert %s: %w", date, err)
			}
			fmt.Printf("New row in spam_by_date: %s = %d\n", date, newCount)
		case err != nil:
			return fmt.Errorf("select %s: %w", date, err)
		case existingCount != newCount:
			if _, err := updateStmt.Exec(newCount, date); err != nil {
				return fmt.Errorf("update %s: %w", date, err)
			}
			fmt.Printf("Updated row in spam_by_date: %s %d -> %d\n", date, existingCount, newCount)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
