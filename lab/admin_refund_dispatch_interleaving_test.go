package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		t.Run("attempt-"+strconv.Itoa(attempt), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			dir := t.TempDir()
			commercePath, providerPath := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
			first, err := Open(commercePath, providerPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = first.Close() })
			if err := first.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			purchase := purchaseHundred(t, first, "refund-dispatch-race", "refund-dispatch-race-checkout")
			if _, err := first.DispatchNext(ctx, ""); err != nil {
				t.Fatal(err)
			}
			correction, err := first.PostReduction(ctx, purchase.InvoiceID, 1000, "refund dispatch race", "refund-dispatch-race-reduction")
			if err != nil || len(correction.GrantIDs) != 1 {
				t.Fatalf("correction: %+v %v", correction, err)
			}
			grantID := correction.GrantIDs[0]
			firstRefundID, err := first.ReserveRefund(ctx, grantID, 400, "refund-dispatch-race-first")
			if err != nil {
				t.Fatal(err)
			}
			second, err := Open(commercePath, providerPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = second.Close() })
			if err := second.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			amount, empty := json.RawMessage(`{"amount_minor":"500"}`), json.RawMessage(`{}`)
			reservePreview, err := first.AdminCreatePreview(ctx, "local-admin", "C15", grantID, amount)
			if err != nil {
				t.Fatal(err)
			}
			reserve, _, err := first.AdminSubmitCommand(ctx, "local-admin", "refund-dispatch-race-reserve", "C15", grantID, amount, reservePreview.ID)
			if err != nil {
				t.Fatal(err)
			}
			dispatchPreview, err := second.AdminCreatePreview(ctx, "local-admin", "C16", firstRefundID, empty)
			if err != nil {
				t.Fatal(err)
			}
			dispatch, _, err := second.AdminSubmitCommand(ctx, "local-admin", "refund-dispatch-race-send", "C16", firstRefundID, empty, dispatchPreview.ID)
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				id      string
				command AdminCommand
				err     error
			}
			start := make(chan struct{})
			ready := make(chan struct{}, 2)
			results := make(chan result, 2)
			for _, item := range []struct {
				lab     *Lab
				command AdminCommand
			}{{first, reserve}, {second, dispatch}} {
				go func(l *Lab, c AdminCommand) {
					ready <- struct{}{}
					<-start
					settled, err := l.AdminExecuteCommand(ctx, c.ID)
					results <- result{c.ID, settled, err}
				}(item.lab, item.command)
			}
			<-ready
			<-ready
			close(start)
			for range 2 {
				select {
				case outcome := <-results:
					if outcome.err != nil {
						t.Fatalf("concurrent command %s (reservation=%t): %v", outcome.id, outcome.id == reserve.ID, outcome.err)
					}
					if outcome.id == reserve.ID {
						reserve = outcome.command
					} else {
						dispatch = outcome.command
					}
				case <-ctx.Done():
					t.Fatalf("concurrent commands timed out: %v", ctx.Err())
				}
			}
			if dispatch.Status != "succeeded" {
				t.Fatalf("dispatch: %+v", dispatch)
			}
			if reserve.Status != "succeeded" && (reserve.Status != "failed" || reserve.ErrorCode != "PREVIEW_STALE") {
				t.Fatalf("reservation: %+v", reserve)
			}
			refund, err := first.Refund(ctx, firstRefundID)
			if err != nil || refund.Status != "succeeded" {
				t.Fatalf("first refund: %+v %v", refund, err)
			}
			credit, err := first.CreditBalance(ctx, grantID)
			if err != nil || credit.RefundedMinor != 400 {
				t.Fatalf("credit: %+v %v", credit, err)
			}
			var refunds, receipts, providerRefunds int
			var refundProviderKey, sourceProviderKey string
			if err := first.db.QueryRowContext(ctx, `SELECT provider_key,source_provider_key FROM refund_operations WHERE id=?`, firstRefundID).Scan(&refundProviderKey, &sourceProviderKey); err != nil {
				t.Fatal(err)
			}
			if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refund_operations WHERE grant_id=?`, grantID).Scan(&refunds); err != nil {
				t.Fatal(err)
			}
			if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, reserve.ID, dispatch.ID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
				t.Fatal(err)
			}
			var recordedSource, recordedCurrency, recordedStatus string
			var recordedAmount int64
			if err := first.provider.db.QueryRowContext(ctx, `SELECT source_capture_key,amount_minor,currency,status FROM refunds WHERE provider_key=?`, refundProviderKey).Scan(&recordedSource, &recordedAmount, &recordedCurrency, &recordedStatus); err != nil {
				t.Fatal(err)
			}
			if providerRefunds != 1 || receipts != 1+boolToInt(reserve.Status == "succeeded") || refunds != 1+boolToInt(reserve.Status == "succeeded") {
				t.Fatalf("refund effect: reserve=%s provider=%d receipts=%d refunds=%d", reserve.Status, providerRefunds, receipts, refunds)
			}
			if recordedSource != sourceProviderKey || recordedAmount != 400 || recordedCurrency != "USD" || recordedStatus != "succeeded" {
				t.Fatalf("provider refund mismatch: source=%s amount=%d currency=%s status=%s", recordedSource, recordedAmount, recordedCurrency, recordedStatus)
			}
			if reserve.Status == "succeeded" && (credit.ReservedMinor != 500 || credit.AvailableMinor != 100) {
				t.Fatalf("winning reservation budget: %+v", credit)
			}
			if reserve.Status == "failed" && (credit.ReservedMinor != 0 || credit.AvailableMinor != 600) {
				t.Fatalf("stale reservation budget: %+v", credit)
			}
		})
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
