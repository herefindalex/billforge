package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminUsageCloseLateDebitAndCreditNoteMatchesDomain(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt := paidProSubscription(t, domain, "usage-parity")
	adminReceipt := paidProSubscription(t, admin, "usage-parity")
	domainSub, adminSub := domainReceipt.SubscriptionID, adminReceipt.SubscriptionID
	runAdmin := func(action, target, key string, payload json.RawMessage, previewRequired bool) {
		t.Helper()
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
			if err != nil {
				t.Fatalf("%s preview: %v", action, err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
	}
	marshal := func(value any) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	compareRating := func(revision int, quantity, rounded, delta int64) {
		t.Helper()
		read := func(l *Lab, subID string) string {
			t.Helper()
			var gotQuantity, gotOverage, numerator, denominator, gotRounded, gotDelta int64
			var version string
			if err := l.db.QueryRowContext(ctx, `SELECT price_version_id,quantity,overage_quantity,exact_minor_numerator,exact_minor_denominator,rounded_minor,delta_minor FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=?`, subID, revision).Scan(&version, &gotQuantity, &gotOverage, &numerator, &denominator, &gotRounded, &gotDelta); err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%s/%d/%d/%d/%d/%d/%d", version, gotQuantity, gotOverage, numerator, denominator, gotRounded, gotDelta)
		}
		d, a := read(domain, domainSub), read(admin, adminSub)
		wantOverage := quantity - 20000
		want := fmt.Sprintf("pro-v1/%d/%d/%d/10/%d/%d", quantity, wantOverage, wantOverage, rounded, delta)
		if d != want || a != want {
			t.Fatalf("usage rating revision %d differs: domain=%s admin=%s want=%s", revision, d, a, want)
		}
	}
	comparePeriod := func(index int, expectedMinor, expectedApplied int64, expectedUsageLine string) {
		t.Helper()
		d, a := loadRenewalFactsAt(t, domain, domainSub, index), loadRenewalFactsAt(t, admin, adminSub, index)
		if !reflect.DeepEqual(d, a) || a.InvoiceMinor != expectedMinor || a.Allocated != expectedApplied {
			t.Fatalf("billing period %d differs: domain=%+v admin=%+v", index, d, a)
		}
		if expectedUsageLine != "" {
			found := false
			for _, line := range a.InvoiceLines {
				if line == expectedUsageLine {
					found = true
				}
			}
			if !found {
				t.Fatalf("period %d missing original-period usage line %q: %v", index, expectedUsageLine, a.InvoiceLines)
			}
		}
	}
	compareEvents := func() {
		t.Helper()
		read := func(l *Lab, subID string) []string {
			t.Helper()
			rows, err := l.db.QueryContext(ctx, `SELECT source,event_id,quantity,period_index,price_version_id,correction_source,correction_event_id FROM usage_events WHERE subscription_id=? ORDER BY source,event_id`, subID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var result []string
			for rows.Next() {
				var source, eventID, version, correctionSource, correctionEvent string
				var quantity int64
				var period int
				if err := rows.Scan(&source, &eventID, &quantity, &period, &version, &correctionSource, &correctionEvent); err != nil {
					t.Fatal(err)
				}
				result = append(result, fmt.Sprintf("%s/%s/%d/%d/%s/%s/%s", source, eventID, quantity, period, version, correctionSource, correctionEvent))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return result
		}
		if d, a := read(domain, domainSub), read(admin, adminSub); !reflect.DeepEqual(d, a) {
			t.Fatalf("usage events differ: domain=%v admin=%v", d, a)
		}
	}

	now = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	firstAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := domain.RecordUsage(ctx, "worker", "usage-parity-first", domainSub, "tasks", 20003, firstAt); err != nil {
		t.Fatal(err)
	}
	runAdmin("C26", "", "usage-parity-record-first", marshal(AdminRecordUsagePayload{
		Source: "worker", EventID: "usage-parity-first", SubscriptionID: adminSub, MeterID: "tasks",
		OccurredAt: firstAt.Format(time.RFC3339Nano), Quantity: "20003",
	}), false)
	compareEvents()
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := domain.CloseUsagePeriod(ctx, domainSub, 0, now); err != nil {
		t.Fatal(err)
	}
	runAdmin("C28", adminSub, "usage-parity-close", json.RawMessage(`{"period_index":"0","cutoff":"2026-10-01T00:00:00Z"}`), true)
	compareRating(1, 20003, 0, 0)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 10000 {
		t.Fatalf("domain October renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "usage-parity-october-renewal", json.RawMessage(`{}`), true)
	comparePeriod(1, 10000, 0, "")
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	now = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	lateAt := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	if _, err := domain.RecordUsage(ctx, "worker", "usage-parity-late", domainSub, "tasks", 7, lateAt); err != nil {
		t.Fatal(err)
	}
	runAdmin("C26", "", "usage-parity-record-late", marshal(AdminRecordUsagePayload{
		Source: "worker", EventID: "usage-parity-late", SubscriptionID: adminSub, MeterID: "tasks",
		OccurredAt: lateAt.Format(time.RFC3339Nano), Quantity: "7",
	}), false)
	compareEvents()
	if _, err := domain.RerateUsagePeriod(ctx, domainSub, 0); err != nil {
		t.Fatal(err)
	}
	runAdmin("C29", adminSub, "usage-parity-rerate-late", json.RawMessage(`{"period_index":"0"}`), true)
	compareRating(2, 20010, 1, 1)
	now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 10001 {
		t.Fatalf("domain November renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "usage-parity-november-renewal", json.RawMessage(`{}`), true)
	comparePeriod(2, 10001, 0, "usage:tasks:period:0:1")
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	comparePeriod(2, 10001, 10001, "usage:tasks:period:0:1")
	now = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if _, err := domain.RecordUsageAdjustment(ctx, "worker", "usage-parity-adjustment", domainSub, "worker", "usage-parity-late", 7); err != nil {
		t.Fatal(err)
	}
	runAdmin("C27", "", "usage-parity-adjust", marshal(adminUsageAdjustmentPayload{
		Source: "worker", EventID: "usage-parity-adjustment", SubscriptionID: adminSub,
		OriginalSource: "worker", OriginalEventID: "usage-parity-late", ReverseQuantity: "7",
	}), true)
	compareEvents()
	if _, err := domain.RerateUsagePeriod(ctx, domainSub, 0); err != nil {
		t.Fatal(err)
	}
	runAdmin("C29", adminSub, "usage-parity-rerate-reversal", json.RawMessage(`{"period_index":"0"}`), true)
	compareRating(3, 20003, 0, -1)
	now = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if held, err := domain.RunRenewals(ctx); err != nil || len(held) != 0 {
		t.Fatalf("domain negative usage hold: %+v err=%v", held, err)
	}
	runAdmin("C44", "", "usage-parity-credit-hold", json.RawMessage(`{}`), true)
	for _, item := range []struct {
		lab *Lab
		sub string
	}{{domain, domainSub}, {admin, adminSub}} {
		var periods int
		if err := item.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=? AND period_index=3`, item.sub).Scan(&periods); err != nil || periods != 0 {
			t.Fatalf("negative usage renewed before credit note: count=%d err=%v", periods, err)
		}
	}
	if notes, err := domain.RunUsageCreditNotes(ctx); err != nil || len(notes) != 1 || notes[0].ReductionMinor != 1 {
		t.Fatalf("domain usage credit note: %+v err=%v", notes, err)
	}
	runAdmin("C30", "", "usage-parity-credit-note", json.RawMessage(`{}`), true)
	for _, item := range []struct {
		lab *Lab
		sub string
	}{{domain, domainSub}, {admin, adminSub}} {
		var funded int64
		if err := item.lab.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(g.amount_minor),0) FROM credit_grants g JOIN billing_periods p ON p.invoice_id=g.source_invoice_id WHERE p.subscription_id=? AND p.period_index=2`, item.sub).Scan(&funded); err != nil || funded != 1 {
			t.Fatalf("funded usage credit=%d err=%v", funded, err)
		}
	}
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 10000 {
		t.Fatalf("domain renewal after credit note: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "usage-parity-december-renewal", json.RawMessage(`{}`), true)
	comparePeriod(3, 10000, 0, "")
}
