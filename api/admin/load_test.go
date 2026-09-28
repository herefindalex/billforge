package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"billforge/lab"
)

// BenchmarkAdminHTTPReadLoad measures the real HTTP handler and SQLite path.
// Run with: go test ./api/admin -run '^$' -bench BenchmarkAdminHTTPReadLoad -benchtime=1x -count=1 -v
func BenchmarkAdminHTTPReadLoad(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "commerce.db")
	l, err := lab.Open(dbPath, filepath.Join(b.TempDir(), "provider.db"), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	quote, err := l.CreateQuoteWithSeats(ctx, "load-business-customer", "pro", 5)
	if err != nil {
		b.Fatal(err)
	}
	receipt, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "load-business-purchase")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, receipt.OperationID, ""); err != nil {
		b.Fatal(err)
	}
	h, err := New(l, Config{Username: "loadtest", Password: "local-load-test-password"})
	if err != nil {
		b.Fatal(err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	queries := []string{
		`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<9999)
		 INSERT INTO quotes(id,customer_id,price_version_id,amount_minor,currency,expires_at,fingerprint)
		 SELECT 'load-quote-'||n,'load-customer-'||n,'basic-v1',2000,'USD',0,'load-fingerprint-'||n FROM seq`,
		`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<9999)
		 INSERT INTO subscriptions(id,quote_id,customer_id,price_version_id,status,created_at)
		 SELECT 'load-sub-'||n,'load-quote-'||n,'load-customer-'||n,'basic-v1','active',0 FROM seq`,
		`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<100000)
		 INSERT INTO usage_events(tenant_id,source,event_id,payload_hash,subscription_id,meter_id,quantity,event_at,received_at,period_index,price_version_id)
		 SELECT 'load','benchmark','load-event-'||n,'load-hash-'||n,'load-sub-1','benchmark-meter',1,0,0,0,'basic-v1' FROM seq`,
	}
	for _, query := range queries {
		if _, err := tx.Exec(query); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	var subscriptions, usageEvents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscriptions`).Scan(&subscriptions); err != nil {
		b.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&usageEvents); err != nil {
		b.Fatal(err)
	}
	if subscriptions != 10000 || usageEvents != 100000 {
		b.Fatalf("load fixture has %d subscriptions and %d usage events, want 10000 and 100000", subscriptions, usageEvents)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	response, err := client.Get(server.URL + "/admin/api/session/csrf")
	if err != nil {
		b.Fatal(err)
	}
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&csrf); err != nil {
		response.Body.Close()
		b.Fatal(err)
	}
	response.Body.Close()
	loginBody := []byte(`{"username":"loadtest","password":"local-load-test-password"}`)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/session", bytes.NewReader(loginBody))
	if err != nil {
		b.Fatal(err)
	}
	request.Header.Set("Origin", server.URL)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf.Token)
	response, err = client.Do(request)
	if err != nil {
		b.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b.Fatalf("login status = %d", response.StatusCode)
	}
	readPage := func(resource string) (time.Duration, int, error) {
		start := time.Now()
		response, err := client.Get(server.URL + "/admin/api/" + resource + "?limit=100")
		if err != nil {
			return 0, 0, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return 0, 0, err
		}
		if response.StatusCode != http.StatusOK {
			return 0, 0, fmt.Errorf("%s status %d: %s", resource, response.StatusCode, body)
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return 0, 0, err
		}
		if len(page.Items) != 100 {
			return 0, 0, fmt.Errorf("%s returned %d rows", resource, len(page.Items))
		}
		return time.Since(start), len(body), nil
	}
	for _, resource := range []string{"subscriptions", "usage-events", "customers"} {
		latency, size, err := readPage(resource)
		if err != nil {
			b.Fatal(err)
		}
		if size > 1<<20 {
			b.Fatalf("%s response exceeds 1 MiB: %d", resource, size)
		}
		b.Logf("%s: 100 rows, %d bytes, %s", resource, size, latency)
	}
	for _, path := range []string{"customers/load-customer-1", "subscriptions/load-sub-1", "quotes/load-quote-1"} {
		start := time.Now()
		response, err := client.Get(server.URL + "/admin/api/" + path)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			b.Fatalf("%s detail status=%d err=%v: %s", path, response.StatusCode, err, body)
		}
		b.Logf("%s: %d bytes, %s", path, len(body), time.Since(start))
	}
	b.ResetTimer()
	for iteration := range b.N {
		var wg sync.WaitGroup
		startWorkers := make(chan struct{})
		var workerDuration time.Duration
		latencies := make(chan time.Duration, 100)
		errors := make(chan error, 1)
		report := func(err error) {
			select {
			case errors <- err:
			default:
			}
		}
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-startWorkers
				for range 20 {
					latency, size, err := readPage("usage-events")
					if err != nil {
						report(err)
						return
					}
					if size > 1<<20 {
						report(fmt.Errorf("response exceeds 1 MiB: %d", size))
						return
					}
					latencies <- latency
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startWorkers
			started := time.Now()
			for event := range 20 {
				eventID := fmt.Sprintf("load-worker-%d-%d", iteration, event)
				if _, err := l.RecordUsage(ctx, "load-business-worker", eventID, receipt.SubscriptionID, "tasks", 1, time.Now().UTC()); err != nil {
					report(err)
					return
				}
			}
			workerDuration = time.Since(started)
		}()
		close(startWorkers)
		wg.Wait()
		close(latencies)
		select {
		case err := <-errors:
			b.Fatal(err)
		default:
		}
		all := make([]time.Duration, 0, 100)
		for latency := range latencies {
			all = append(all, latency)
		}
		if len(all) != 100 {
			b.Fatalf("expected 100 reads, got %d", len(all))
		}
		var committedWrites int
		if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE source='load-business-worker'`).Scan(&committedWrites); err != nil {
			b.Fatal(err)
		}
		if committedWrites != 20*(iteration+1) {
			b.Fatalf("worker progress = %d, want %d", committedWrites, 20*(iteration+1))
		}
		sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
		b.Logf("HTTP 100-row read p95=%s max=%s; business worker committed=%d duration=%s", all[94], all[99], committedWrites, workerDuration)
		if all[94] > 500*time.Millisecond {
			b.Fatalf("HTTP 100-row read p95=%s exceeds 500ms", all[94])
		}
	}
}
