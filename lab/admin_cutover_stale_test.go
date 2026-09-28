package lab

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAdminCutoverPreviewRejectsReadinessLostBeforeExecution(t *testing.T) {
	for _, actionID := range []string{"C41", "C42"} {
		t.Run(actionID, func(t *testing.T) {
			l, _, _ := openTestLab(t)
			ctx := context.Background()
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			const accountID = "stale-cutover-account"
			if _, err := l.LinkLegacyAccount(ctx, AccountLink{
				LegacyAccountID: accountID, CustomerID: "stale-cutover-customer",
				BeneficiaryID: "stale-cutover-beneficiary", Cohort: "default",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := l.ShadowQuote(ctx, accountID, "basic", 0, 2000, "USD"); err != nil {
				t.Fatal(err)
			}
			if _, err := l.ShadowEntitlement(ctx, accountID, "future-sub", "missing"); err != nil {
				t.Fatal(err)
			}
			if _, err := l.RunReconciliation(ctx, fixedNow); err != nil {
				t.Fatal(err)
			}
			payload := json.RawMessage(`{"max_quote_p95_millis":"5000","max_unknown_payments":"0","max_open_discrepancies":"0"}`)
			if actionID == "C42" {
				readPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C41", accountID, payload)
				if err != nil {
					t.Fatal(err)
				}
				readCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-cutover-read-first", "C41", accountID, payload, readPreview.ID)
				if err != nil {
					t.Fatal(err)
				}
				readCommand, err = l.AdminExecuteCommand(ctx, readCommand.ID)
				if err != nil || readCommand.Status != "succeeded" {
					t.Fatalf("read cutover=%+v err=%v", readCommand, err)
				}
			}
			preview, err := l.AdminCreatePreview(ctx, "local-admin", actionID, accountID, payload)
			if err != nil {
				t.Fatal(err)
			}
			shadow, err := l.ShadowQuote(ctx, accountID, "basic", 0, 2500, "USD")
			if err != nil || shadow.Matched {
				t.Fatalf("readiness loss shadow=%+v err=%v", shadow, err)
			}
			command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-cutover-"+actionID, actionID, accountID, payload, preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			command, err = l.AdminExecuteCommand(ctx, command.ID)
			if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
				t.Fatalf("readiness changed command=%+v err=%v", command, err)
			}
			link, err := l.AccountLink(ctx, accountID)
			if err != nil {
				t.Fatal(err)
			}
			wantRead := "legacy"
			if actionID == "C42" {
				wantRead = "commerce"
			}
			var receipts int
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if link.ReadOwner != wantRead || link.WriterOwner != "legacy" || receipts != 0 {
				t.Fatalf("owners=%s/%s receipts=%d", link.ReadOwner, link.WriterOwner, receipts)
			}
			if _, err := l.AdminCreatePreview(ctx, "local-admin", actionID, accountID, payload); !errors.Is(err, ErrConflict) {
				t.Fatalf("lost readiness still allowed preview: %v", err)
			}
		})
	}
}
