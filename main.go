package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

const (
	defaultDBHost    = "crt.sh"
	defaultDBPort    = 5432
	defaultDBUser    = "guest"
	defaultDBName    = "certwatch"
	defaultDBSSLMode = "disable"
	queryTmpl        = `
		SELECT ci.NAME_VALUE
		FROM certificate_and_identities ci
		WHERE ci.NAME_TYPE = 'commonName'
		AND reverse(lower(ci.NAME_VALUE)) LIKE reverse(lower($1))
	`
)

var (
	db      *sql.DB
	stmt    *sql.Stmt
	dbHost  string
	connStr string
)

func main() {
	var err error

	// Initialize database connection
	connStr = fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=%s",
		dbHost, defaultDBPort, defaultDBUser, defaultDBName, defaultDBSSLMode)
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	stmt, err = db.Prepare(queryTmpl)
	if err != nil {
		log.Fatalf("Failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

var rootCmd = &cobra.Command{
	Use:   "crtsh_subdomains <domain>",
	Short: "Fetch and process common names from crt.sh for a domain suffix",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return run(args[0])
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&dbHost, "db-host", defaultDBHost, "PostgreSQL database host")
}

func run(domain string) error {
	if domain == "" {
		return nil
	}
	pattern := "%." + domain
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	names, err := fetchCommonNames(ctx, pattern)
	if err != nil {
		return fmt.Errorf("failed to fetch names: %w", err)
	}

	processed := make(map[string]struct{})
	processed[domain] = struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		name = strings.TrimPrefix(name, "*.")
		if name != "" {
			processed[name] = struct{}{}
		}
	}

	var results []string
	for name := range processed {
		results = append(results, name)
	}
	sort.Strings(results)

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for _, name := range results {
		fmt.Fprintln(w, name)
	}
	return nil
}

func fetchCommonNames(ctx context.Context, pattern string) ([]string, error) {
	rows, err := stmt.QueryContext(ctx, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make([]string, 0, 100) // Pre-allocate with estimated capacity
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names = append(names, name)
	}

	return names, rows.Err()
}
