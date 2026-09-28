package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// BenchmarkAdminStateLoad records the cost of the current aggregate read path.
// Run with: go test ./lab -run '^$' -bench BenchmarkAdminStateLoad -benchtime=1x
func BenchmarkAdminStateLoad(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		b.Fatal(err)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<10000)
	INSERT INTO quotes(id,customer_id,price_version_id,amount_minor,currency,expires_at,fingerprint)
	SELECT 'load-quote-'||n,'load-customer-'||n,'basic-v1',2000,'USD',?, 'load-fingerprint-'||n FROM seq`, time.Now().Add(time.Hour).UnixNano()); err != nil {
		b.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<10000)
	INSERT INTO subscriptions(id,quote_id,customer_id,price_version_id,status,created_at)
	SELECT 'load-sub-'||n,'load-quote-'||n,'load-customer-'||n,'basic-v1','active',? FROM seq`, time.Now().UnixNano()); err != nil {
		b.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<100000)
	INSERT INTO usage_events(tenant_id,source,event_id,payload_hash,subscription_id,meter_id,quantity,event_at,received_at,period_index,price_version_id)
	SELECT 'load','benchmark','load-event-'||n,'load-hash-'||n,'load-sub-1','benchmark-meter',1,?,?,0,'basic-v1' FROM seq`, time.Now().UnixNano(), time.Now().UnixNano()); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.Run("aggregate-state", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			state, err := l.State(ctx)
			if err != nil {
				b.Fatal(err)
			}
			if len(state.Subscriptions) != 10000 || len(state.UsageEvents) != 100000 {
				b.Fatalf("fixture incomplete: %d %d", len(state.Subscriptions), len(state.UsageEvents))
			}
		}
	})
	b.Run("usage-page-100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, _, _, err := l.AdminResourcePage(ctx, "usage-events", 0, 100)
			if err != nil || len(items) != 100 {
				b.Fatalf("usage page: %d %v", len(items), err)
			}
		}
	})
	b.Run("subscription-page-100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, _, _, err := l.AdminResourcePage(ctx, "subscriptions", 0, 100)
			if err != nil || len(items) != 100 {
				b.Fatalf("subscription page: %d %v", len(items), err)
			}
		}
	})
	b.Run("entitlement-preview-100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			preview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
			if err != nil {
				b.Fatal(err)
			}
			var source adminMaintenanceSource
			if err := json.Unmarshal(preview.SourceVersions, &source); err != nil || len(source.Items) != 100 {
				b.Fatalf("entitlement preview: %d %v", len(source.Items), err)
			}
		}
	})
	b.Run("entitlement-preview-rotated-100", func(b *testing.B) {
		first, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
		if err != nil {
			b.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "benchmark-entitlement-rotation", "C45", "", json.RawMessage(`{}`), first.ID)
		if err != nil {
			b.Fatal(err)
		}
		if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			preview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
			if err != nil {
				b.Fatal(err)
			}
			var source adminMaintenanceSource
			if err := json.Unmarshal(preview.SourceVersions, &source); err != nil || len(source.Items) != 100 {
				b.Fatalf("rotated entitlement preview: %d %v", len(source.Items), err)
			}
		}
	})
	b.Run("five-readers-one-writer", func(b *testing.B) {
		for run := 0; run < b.N; run++ {
			var wg sync.WaitGroup
			latencies := make(chan time.Duration, 100)
			errors := make(chan error, 1)
			for reader := 0; reader < 5; reader++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 20; i++ {
						start := time.Now()
						items, _, _, err := l.AdminResourcePage(ctx, "usage-events", 0, 100)
						if err != nil || len(items) != 100 {
							select {
							case errors <- fmt.Errorf("page has %d items: %w", len(items), err):
							default:
							}
							return
						}
						latencies <- time.Since(start)
						if i == 0 {
							encoded, _ := json.Marshal(items)
							if len(encoded) > 1<<20 {
								select {
								case errors <- fmt.Errorf("page exceeds 1 MiB: %d", len(encoded)):
								default:
								}
								return
							}
						}
					}
				}()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					if _, err := l.db.ExecContext(ctx, `INSERT INTO admin_audit(actor_id,action_id,reason,recorded_at) VALUES('load','probe','worker',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
						select {
						case errors <- err:
						default:
						}
						return
					}
				}
			}()
			wg.Wait()
			close(latencies)
			select {
			case err := <-errors:
				b.Fatalf("concurrent load: %v", err)
			default:
			}
			var all []time.Duration
			for latency := range latencies {
				all = append(all, latency)
			}
			if len(all) != 100 {
				b.Fatalf("expected 100 reads, got %d", len(all))
			}
			sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
			b.Logf("100-row read p95=%s max=%s", all[94], all[99])
		}
	})
}
