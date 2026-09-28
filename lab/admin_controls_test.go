package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminFaultTicketsKeepPendingTicketVisibleAfterRecentUsedTickets(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fault-list-owner", "C01", "", json.RawMessage(`{"customer_id":"fault-list","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO admin_fault_tickets(id,operation_kind,operation_id,mode,created_at)
		VALUES('pending-old','payment','payment-pending','lost_response','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("used-%03d", i)
		if _, err := l.db.ExecContext(ctx, `INSERT INTO admin_fault_tickets(id,operation_kind,operation_id,mode,claimed_command_id,created_at)
			VALUES(?,'payment',?,'lost_response',?,'2026-02-01T00:00:00Z')`, id, id, command.ID); err != nil {
			t.Fatal(err)
		}
	}
	tickets, err := l.AdminFaultTickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 100 {
		t.Fatalf("bounded fault ticket list has %d rows, want 100", len(tickets))
	}
	if tickets[0].ID != "pending-old" || tickets[0].ClaimedCommandID != "" {
		t.Fatalf("pending ticket lost from bounded list: first=%+v total=%d", tickets[0], len(tickets))
	}
	seen := make(map[string]bool)
	var cursor *AdminFaultTicketCursor
	for page := 0; ; page++ {
		items, next, err := l.AdminFaultTicketsPage(ctx, cursor, 20)
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 {
			if len(items) != 20 || items[0].ID != "pending-old" {
				t.Fatalf("first fault page lost pending ticket: %+v", items)
			}
			if _, err := l.db.ExecContext(ctx, `INSERT INTO admin_fault_tickets(id,operation_kind,operation_id,mode,created_at)
				VALUES('pending-new','payment','payment-new','lost_response','2026-03-01T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range items {
			if seen[item.ID] {
				t.Fatalf("ticket %q repeated across pages", item.ID)
			}
			seen[item.ID] = true
		}
		if next == nil {
			break
		}
		cursor = next
	}
	if len(seen) != 101 || seen["pending-new"] {
		t.Fatalf("cursor changed membership after an earlier insert: count=%d new=%v", len(seen), seen["pending-new"])
	}
}

func TestAdminFaultTicketOrderDoesNotDependOnTimestampTextPrecision(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, at string }{
		{"exact-second", "2026-01-01T00:00:00Z"},
		{"fractional-early", "2026-01-01T00:00:00.1Z"},
		{"fractional-late", "2026-01-01T00:00:00.9Z"},
	} {
		if _, err := l.db.ExecContext(ctx, `INSERT INTO admin_fault_tickets(id,operation_kind,operation_id,mode,created_at)
			VALUES(?,'payment',?,'lost_response',?)`, row.id, row.id, row.at); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := l.AdminFaultTicketsPage(ctx, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].ID != "fractional-late" || items[1].ID != "fractional-early" || items[2].ID != "exact-second" {
		t.Fatalf("ticket order changed with timestamp precision: %+v", items)
	}
}

func submitControl(t *testing.T, l *Lab, key, actionID, targetID string, payload json.RawMessage) AdminCommand {
	t.Helper()
	ctx := context.Background()
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, actionID, targetID, payload, "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("control %s: %+v %v", actionID, command, err)
	}
	return command
}

func TestAdminClockPersistsAndControlsRunningDomainClock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l, err := Open(commerce, provider, func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paidProSubscription(t, l, "clock-revision-customer")
	stalePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	command := submitControl(t, l, "clock-fixed-001", "C46", "", json.RawMessage(`{"mode":"fixed","value_utc":"2026-10-01T12:00:00Z"}`))
	if !l.ClockTime().Equal(fixed) || !l.now().Equal(fixed) {
		t.Fatalf("running clock did not update: %v", l.ClockTime())
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "clock-stale-preview-001", "C45", "", json.RawMessage(`{}`), stalePreview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("clock revision did not invalidate preview: %v", err)
	}
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT clock_revision FROM admin_commands WHERE id=?`, command.ID).Scan(&revision); err != nil || revision != 0 {
		t.Fatalf("first revision %d %v", revision, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !l.ClockTime().Equal(fixed) {
		t.Fatalf("clock not restored: %v", l.ClockTime())
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := l.AdminClock(ctx)
	if err != nil || state.Mode != "fixed" || state.Revision != 1 {
		t.Fatalf("clock state %+v %v", state, err)
	}
	command = submitControl(t, l, "clock-real-001", "C46", "", json.RawMessage(`{"mode":"real"}`))
	if !l.ClockTime().Equal(base) {
		t.Fatalf("real clock not restored: %v", l.ClockTime())
	}
	if err := l.db.QueryRowContext(ctx, `SELECT clock_revision FROM admin_commands WHERE id=?`, command.ID).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("second revision %d %v", revision, err)
	}
}

func TestAdminProviderControlReceiptAndOneShotFault(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "control-customer-1", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-checkout-1")
	if err != nil {
		t.Fatal(err)
	}
	decision := submitControl(t, l, "decision-payment-001", "C47", paid.OperationID, json.RawMessage(`{"status":"definitively_failed"}`))
	var receiptCount int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE command_id=?`, decision.ID).Scan(&receiptCount); err != nil || receiptCount != 1 {
		t.Fatalf("provider control receipt %d %v", receiptCount, err)
	}
	if _, err := l.AdminExecuteCommand(ctx, decision.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, paid.OperationID).Scan(&status); err != nil || status != "definitively_failed" {
		t.Fatalf("payment status %q %v", status, err)
	}

	quote, err = l.CreateQuote(ctx, "control-customer-2", "basic")
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-checkout-2")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "fault-payment-001", "C49", second.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", second.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-fault-001", "C09", second.OperationID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = l.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "waiting_verification" {
		t.Fatalf("fault dispatch %+v %v", dispatch, err)
	}
	var claimed string
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, second.OperationID).Scan(&claimed); err != nil || claimed != dispatch.ID {
		t.Fatalf("fault ticket claim %q %v", claimed, err)
	}
	if _, err := l.AdminExecuteCommand(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_id=?`, second.OperationID).Scan(&receiptCount); err != nil || receiptCount != 1 {
		t.Fatalf("fault ticket count %d %v", receiptCount, err)
	}
	quote, err = l.CreateQuote(ctx, "control-customer-3", "basic")
	if err != nil {
		t.Fatal(err)
	}
	third, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-checkout-3")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "fault-payment-crash-001", "C49", third.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"crash_after_provider"}`))
	preview, err = l.AdminCreatePreview(ctx, "local-admin", "C09", third.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	crashCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-crash-001", "C09", third.OperationID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, crashCommand.ID); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("expected injected crash: %v", err)
	}
	crashCommand, err = l.AdminExecuteCommand(ctx, crashCommand.ID)
	if err != nil || crashCommand.Status != "succeeded" {
		t.Fatalf("crash retry %+v %v", crashCommand, err)
	}
}

func TestAdminFaultTicketOnlyAffectsItsSelectedPayment(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	makePayment := func(customer, key string) string {
		t.Helper()
		quote, err := l.CreateQuote(ctx, customer, "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, key)
		if err != nil {
			t.Fatal(err)
		}
		return accepted.OperationID
	}
	dispatch := func(operationID, key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", operationID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C09", operationID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}

	unticketed := makePayment("fault-isolation-a", "fault-isolation-checkout-a")
	ticketed := makePayment("fault-isolation-b", "fault-isolation-checkout-b")
	submitControl(t, l, "fault-isolation-ticket", "C49", ticketed, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))

	first := dispatch(unticketed, "fault-isolation-dispatch-a")
	if first.Status != "succeeded" {
		t.Fatalf("unselected payment status %q", first.Status)
	}
	var claimed sql.NullString
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, ticketed).Scan(&claimed); err != nil || claimed.Valid {
		t.Fatalf("ticket claimed by unselected payment: %+v %v", claimed, err)
	}
	var captures int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 1 {
		t.Fatalf("captures after unselected dispatch: %d %v", captures, err)
	}

	second := dispatch(ticketed, "fault-isolation-dispatch-b")
	if second.Status != "waiting_verification" {
		t.Fatalf("selected payment status %q", second.Status)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, ticketed).Scan(&claimed); err != nil || !claimed.Valid || claimed.String != second.ID {
		t.Fatalf("selected ticket claim %+v %v", claimed, err)
	}
	second, err := l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("selected payment recovery %+v %v", second, err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 2 {
		t.Fatalf("captures after recovery: %d %v", captures, err)
	}
}

func TestAdminFaultTicketOnlyAffectsItsSelectedRefund(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	makeRefund := func(customer, key string) string {
		t.Helper()
		quote, err := l.CreateQuote(ctx, customer, "basic")
		if err != nil {
			t.Fatal(err)
		}
		paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, key+"-checkout")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
			t.Fatal(err)
		}
		correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "fault isolation", key+"-reduction")
		if err != nil || len(correction.GrantIDs) != 1 {
			t.Fatalf("funded credit: %+v %v", correction, err)
		}
		refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, key+"-reserve")
		if err != nil {
			t.Fatal(err)
		}
		return refundID
	}
	dispatch := func(refundID, key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C16", refundID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	unticketed := makeRefund("refund-fault-isolation-a", "refund-fault-isolation-a")
	ticketed := makeRefund("refund-fault-isolation-b", "refund-fault-isolation-b")
	submitControl(t, l, "refund-fault-isolation-ticket", "C49", ticketed, json.RawMessage(`{"operation_kind":"refund","mode":"lost_response"}`))
	first := dispatch(unticketed, "refund-fault-isolation-dispatch-a")
	if first.Status != "succeeded" {
		t.Fatalf("unselected refund status %q", first.Status)
	}
	var claimed sql.NullString
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_kind='refund' AND operation_id=?`, ticketed).Scan(&claimed); err != nil || claimed.Valid {
		t.Fatalf("ticket claimed by unselected refund: %+v %v", claimed, err)
	}
	second := dispatch(ticketed, "refund-fault-isolation-dispatch-b")
	if second.Status != "waiting_verification" {
		t.Fatalf("selected refund status %q", second.Status)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_kind='refund' AND operation_id=?`, ticketed).Scan(&claimed); err != nil || !claimed.Valid || claimed.String != second.ID {
		t.Fatalf("selected refund ticket claim: %+v %v", claimed, err)
	}
	second, err := l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("selected refund recovery: %+v %v", second, err)
	}
	var refunds int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil || refunds != 2 {
		t.Fatalf("provider refunds after recovery: %d %v", refunds, err)
	}
}

func TestAdminDuplicateFaultTicketFailsWithoutStrandingCommand(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "duplicate-fault-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "duplicate-fault-checkout")
	if err != nil {
		t.Fatal(err)
	}
	firstPayload := json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`)
	first := submitControl(t, l, "duplicate-fault-first", "C49", accepted.OperationID, firstPayload)
	secondPayload := json.RawMessage(`{"operation_kind":"payment","mode":"crash_after_provider"}`)
	second, _, err := l.AdminSubmitCommand(ctx, "local-admin", "duplicate-fault-second", "C49", accepted.OperationID, secondPayload, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "failed" || second.ErrorCode != "DOMAIN_REJECTED" {
		t.Fatalf("duplicate ticket command %+v %v", second, err)
	}
	replayed, found, err := l.AdminSubmitCommand(ctx, "local-admin", "duplicate-fault-second", "C49", accepted.OperationID, secondPayload, "")
	if err != nil || !found || replayed.ID != second.ID || replayed.Status != "failed" {
		t.Fatalf("duplicate key replay %+v found=%v err=%v", replayed, found, err)
	}
	var tickets, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_kind='payment' AND operation_id=?`, accepted.OperationID).Scan(&tickets); err != nil || tickets != 1 {
		t.Fatalf("ticket count %d %v", tickets, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, first.ID, second.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("control receipt count %d %v", receipts, err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := l.AdminSubmitCommand(ctx, "local-admin", "duplicate-fault-dispatch", "C09", accepted.OperationID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = l.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "waiting_verification" {
		t.Fatalf("original ticket dispatch %+v %v", dispatch, err)
	}
	var claimed string
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, accepted.OperationID).Scan(&claimed); err != nil || claimed != dispatch.ID {
		t.Fatalf("original ticket claim %q %v", claimed, err)
	}
}

func TestAdminClaimedFaultSurvivesCrashBeforeProviderDispatch(t *testing.T) {
	for _, tc := range []struct {
		kind, dispatchAction, providerTable string
	}{
		{kind: "payment", dispatchAction: "C09", providerTable: "captures"},
		{kind: "refund", dispatchAction: "C16", providerTable: "refunds"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
			open := func() *Lab {
				t.Helper()
				l, err := Open(commerce, provider, func() time.Time { return fixedNow })
				if err != nil {
					t.Fatal(err)
				}
				if err := l.InitAdmin(ctx); err != nil {
					t.Fatal(err)
				}
				return l
			}
			l := open()
			quote, err := l.CreateQuote(ctx, "claimed-fault-"+tc.kind, "basic")
			if err != nil {
				t.Fatal(err)
			}
			paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "claimed-fault-checkout-"+tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			operationID := paid.OperationID
			if tc.kind == "refund" {
				if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
					t.Fatal(err)
				}
				correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "claimed fault refund", "claimed-fault-reduction")
				if err != nil || len(correction.GrantIDs) != 1 {
					t.Fatalf("correction %+v %v", correction, err)
				}
				operationID, err = l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "claimed-fault-reserve")
				if err != nil {
					t.Fatal(err)
				}
			}
			faultPayload, err := json.Marshal(map[string]string{"operation_kind": tc.kind, "mode": "lost_response"})
			if err != nil {
				t.Fatal(err)
			}
			submitControl(t, l, "claimed-fault-ticket-"+tc.kind, "C49", operationID, faultPayload)
			preview, err := l.AdminCreatePreview(ctx, "local-admin", tc.dispatchAction, operationID, json.RawMessage(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "claimed-fault-dispatch-"+tc.kind, tc.dispatchAction, operationID, json.RawMessage(`{}`), preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Reproduce the durable state after the fault claim commits and before the provider call.
			if _, err := l.db.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=? WHERE operation_kind=? AND operation_id=?`, command.ID, tc.kind, operationID); err != nil {
				t.Fatal(err)
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			l = open()
			defer l.Close()
			command, err = l.AdminExecuteCommand(ctx, command.ID)
			if err != nil || command.Status != "waiting_verification" {
				t.Fatalf("dispatch after restart %+v %v", command, err)
			}
			var claimed string
			if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_kind=? AND operation_id=?`, tc.kind, operationID).Scan(&claimed); err != nil || claimed != command.ID {
				t.Fatalf("fault claim %q %v", claimed, err)
			}
			var facts, receipts int
			if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tc.providerTable).Scan(&facts); err != nil {
				t.Fatal(err)
			}
			if tc.kind == "payment" {
				if facts != 1 {
					t.Fatalf("payment captures after resumed dispatch %d", facts)
				}
			} else if facts != 1 {
				t.Fatalf("refunds after resumed dispatch %d", facts)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 0 {
				t.Fatalf("receipt before verification %d %v", receipts, err)
			}
			command, err = l.AdminExecuteCommand(ctx, command.ID)
			if err != nil || command.Status != "succeeded" {
				t.Fatalf("verification after restart %+v %v", command, err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
				t.Fatalf("receipt after verification %d %v", receipts, err)
			}
			if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tc.providerTable).Scan(&facts); err != nil || facts != 1 {
				t.Fatalf("provider facts after verification %d %v", facts, err)
			}
		})
	}
}

func TestAdminRevokedDispatchReleasesClaimedFaultBeforeProviderCall(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "revoked-claimed-fault", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "revoked-claimed-fault-checkout")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "revoked-claimed-fault-ticket", "C49", paid.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	createDispatch := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", paid.OperationID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C09", paid.OperationID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	first := createDispatch("revoked-claimed-fault-dispatch-first")
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=? WHERE operation_kind='payment' AND operation_id=?`, first.ID, paid.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	first, err = l.AdminCommand(ctx, first.ID)
	if err != nil || first.Status != "failed" || first.ErrorCode != "PERMISSION_REVOKED" {
		t.Fatalf("revoked dispatch %+v %v", first, err)
	}
	var claimed sql.NullString
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, paid.OperationID).Scan(&claimed); err != nil || claimed.Valid {
		t.Fatalf("revoked fault claim %+v %v", claimed, err)
	}
	var captures, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 0 {
		t.Fatalf("capture after revocation %d %v", captures, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, first.ID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("revoked receipt %d %v", receipts, err)
	}
	second := createDispatch("revoked-claimed-fault-dispatch-second")
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "waiting_verification" {
		t.Fatalf("replacement dispatch %+v %v", second, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, paid.OperationID).Scan(&claimed); err != nil || !claimed.Valid || claimed.String != second.ID {
		t.Fatalf("replacement fault claim %+v %v", claimed, err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("replacement verification %+v %v", second, err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 1 {
		t.Fatalf("capture after replacement %d %v", captures, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, second.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("replacement receipt %d %v", receipts, err)
	}
}

func TestAdminRevokedRefundDispatchRetainsReservationAndFault(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "revoked-refund-fault", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "revoked-refund-fault-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "revoked refund fault", "revoked-refund-fault-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction %+v %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "revoked-refund-fault-reserve")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "revoked-refund-fault-ticket", "C49", refundID, json.RawMessage(`{"operation_kind":"refund","mode":"lost_response"}`))
	createDispatch := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C16", refundID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	first := createDispatch("revoked-refund-fault-dispatch-first")
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=? WHERE operation_kind='refund' AND operation_id=?`, first.ID, refundID); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	first, err = l.AdminCommand(ctx, first.ID)
	if err != nil || first.Status != "failed" || first.ErrorCode != "PERMISSION_REVOKED" {
		t.Fatalf("revoked refund dispatch %+v %v", first, err)
	}
	var claimed sql.NullString
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, refundID).Scan(&claimed); err != nil || claimed.Valid {
		t.Fatalf("revoked refund fault claim %+v %v", claimed, err)
	}
	var status string
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT status,amount_minor FROM refund_operations WHERE id=?`, refundID).Scan(&status, &amount); err != nil || status != "created" || amount != 500 {
		t.Fatalf("reserved refund status=%q amount=%d err=%v", status, amount, err)
	}
	var refunds int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil || refunds != 0 {
		t.Fatalf("provider refund after revocation %d %v", refunds, err)
	}
	second := createDispatch("revoked-refund-fault-dispatch-second")
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "waiting_verification" {
		t.Fatalf("replacement refund dispatch %+v %v", second, err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("replacement refund verification %+v %v", second, err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil || refunds != 1 {
		t.Fatalf("provider refunds after replacement %d %v", refunds, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, first.ID, second.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("refund dispatch receipts %d %v", receipts, err)
	}
}

func TestAdminClaimedFaultBlocksCompetingDispatchUntilOwnerResumes(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "competing-claimed-fault", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "competing-claimed-fault-checkout")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "competing-claimed-fault-ticket", "C49", paid.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	createDispatch := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", paid.OperationID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C09", paid.OperationID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	competitor := createDispatch("competing-claimed-fault-competitor")
	owner := createDispatch("competing-claimed-fault-owner")
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=? WHERE operation_kind='payment' AND operation_id=?`, owner.ID, paid.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, competitor.ID); !errors.Is(err, ErrAdminFaultOwnedByOtherCommand) {
		t.Fatalf("competing dispatch must wait for ticket owner: %v", err)
	}
	competitor, err = l.AdminCommand(ctx, competitor.ID)
	if err != nil || competitor.Status != "accepted" {
		t.Fatalf("competing command after conflict %+v %v", competitor, err)
	}
	var captures, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 0 {
		t.Fatalf("provider capture before owner %d %v", captures, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, competitor.ID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("competing receipt before owner %d %v", receipts, err)
	}
	if err := l.AdminResumeWithPolicy(ctx, nil); err != nil {
		t.Fatalf("resume should process ticket owner despite earlier competing command: %v", err)
	}
	owner, err = l.AdminCommand(ctx, owner.ID)
	if err != nil || owner.Status != "waiting_verification" {
		t.Fatalf("owner dispatch %+v %v", owner, err)
	}
	owner, err = l.AdminExecuteCommand(ctx, owner.ID)
	if err != nil || owner.Status != "succeeded" {
		t.Fatalf("owner verification %+v %v", owner, err)
	}
	if err := l.AdminResumeWithPolicy(ctx, nil); err != nil {
		t.Fatalf("resume competing command after owner resolves: %v", err)
	}
	competitor, err = l.AdminCommand(ctx, competitor.ID)
	if err != nil || competitor.Status != "succeeded" {
		t.Fatalf("competing command after owner resolves %+v %v", competitor, err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 1 {
		t.Fatalf("provider captures after owner %d %v", captures, err)
	}
}

func TestAdminStalePreviewReleasesClaimedFault(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "stale-claimed-fault", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "stale-claimed-fault-checkout")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "stale-claimed-fault-ticket", "C49", paid.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", paid.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	stale, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-claimed-fault-dispatch", "C09", paid.OperationID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=? WHERE operation_kind='payment' AND operation_id=?`, stale.ID, paid.OperationID); err != nil {
		t.Fatal(err)
	}
	// Model a source version change after the prior worker claimed the ticket.
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_previews SET source_versions_json='{}' WHERE id=?`, preview.ID); err != nil {
		t.Fatal(err)
	}
	stale, err = l.AdminExecuteCommand(ctx, stale.ID)
	if err != nil || stale.Status != "failed" || stale.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale dispatch %+v %v", stale, err)
	}
	var claimed sql.NullString
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, paid.OperationID).Scan(&claimed); err != nil || claimed.Valid {
		t.Fatalf("stale command retained fault %+v %v", claimed, err)
	}
	var captures, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 0 {
		t.Fatalf("capture after stale command %d %v", captures, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, stale.ID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("stale command receipt %d %v", receipts, err)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", paid.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fresh-claimed-fault-dispatch", "C09", paid.OperationID, json.RawMessage(`{}`), freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = l.AdminExecuteCommand(ctx, fresh.ID)
	if err != nil || fresh.Status != "waiting_verification" {
		t.Fatalf("fresh dispatch %+v %v", fresh, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?`, paid.OperationID).Scan(&claimed); err != nil || !claimed.Valid || claimed.String != fresh.ID {
		t.Fatalf("fresh command did not claim fault %+v %v", claimed, err)
	}
}

func TestAdminConflictingProviderDecisionsPreserveFirstOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, actionID, table, factTable string
	}{
		{name: "payment", actionID: "C47", table: "payment_operations", factTable: "captures"},
		{name: "refund", actionID: "C48", table: "refund_operations", factTable: "refunds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			l, _, _ := openTestLab(t)
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			quote, err := l.CreateQuote(ctx, "conflicting-decision-"+tc.name, "basic")
			if err != nil {
				t.Fatal(err)
			}
			paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "conflicting-decision-checkout-"+tc.name)
			if err != nil {
				t.Fatal(err)
			}
			operationID := paid.OperationID
			if tc.actionID == "C48" {
				if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
					t.Fatal(err)
				}
				correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "conflicting refund decision", "conflicting-decision-reduction")
				if err != nil || len(correction.GrantIDs) != 1 {
					t.Fatalf("correction %+v %v", correction, err)
				}
				operationID, err = l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "conflicting-decision-reserve")
				if err != nil {
					t.Fatal(err)
				}
			}
			first := submitControl(t, l, "decision-success-"+tc.name, tc.actionID, operationID, json.RawMessage(`{"status":"succeeded"}`))
			second, _, err := l.AdminSubmitCommand(ctx, "local-admin", "decision-failure-"+tc.name, tc.actionID, operationID, json.RawMessage(`{"status":"definitively_failed"}`), "")
			if err != nil {
				t.Fatal(err)
			}
			second, err = l.AdminExecuteCommand(ctx, second.ID)
			if err != nil || second.Status != "failed" || second.ErrorCode != "DOMAIN_REJECTED" {
				t.Fatalf("conflicting decision %+v %v", second, err)
			}
			var key string
			if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM `+tc.table+` WHERE id=?`, operationID).Scan(&key); err != nil {
				t.Fatal(err)
			}
			var receipts int
			if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE target_key=?`, key).Scan(&receipts); err != nil || receipts != 1 {
				t.Fatalf("provider control receipts %d %v", receipts, err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, first.ID, second.ID).Scan(&receipts); err != nil || receipts != 1 {
				t.Fatalf("admin receipts %d %v", receipts, err)
			}
			if tc.actionID == "C47" {
				_, err = l.DispatchCapture(ctx, operationID, "")
			} else {
				_, err = l.DispatchRefund(ctx, operationID, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tc.factTable+` WHERE provider_key=? AND status='succeeded'`, key).Scan(&receipts); err != nil || receipts != 1 {
				t.Fatalf("provider facts %d %v", receipts, err)
			}
			late, _, err := l.AdminSubmitCommand(ctx, "local-admin", "decision-after-terminal-"+tc.name, tc.actionID, operationID, json.RawMessage(`{"status":"succeeded"}`), "")
			if err != nil {
				t.Fatal(err)
			}
			late, err = l.AdminExecuteCommand(ctx, late.ID)
			if err != nil || late.Status != "failed" || late.ErrorCode != "DOMAIN_REJECTED" {
				t.Fatalf("decision after terminal operation %+v %v", late, err)
			}
			if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE target_key=?`, key).Scan(&receipts); err != nil || receipts != 1 {
				t.Fatalf("provider control receipts after terminal operation %d %v", receipts, err)
			}
		})
	}
}

func TestAdminRefundDecisionRecoversAfterProviderCommitAndDispatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	open := func() *Lab {
		l, err := Open(commerce, provider, func() time.Time { return fixedNow })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	quote, err := l.CreateQuote(ctx, "refund-control-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "refund-control-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "refund control", "refund-control-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("reduction %+v %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "refund-control-reserve")
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "decision-refund-001", "C48", refundID, json.RawMessage(`{"status":"definitively_failed"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM refund_operations WHERE id=?`, refundID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.adminSetDecision(ctx, command.ID, "refund", key, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchRefund(ctx, refundID, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("recovered control %+v %v", command, err)
	}
	var count int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("provider receipts %d %v", count, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin receipts %d %v", count, err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM refund_operations WHERE id=?`, refundID).Scan(&status); err != nil || status != "definitively_failed" {
		t.Fatalf("refund operation status %q %v", status, err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds WHERE provider_key=? AND status='succeeded'`, key).Scan(&count); err != nil || count != 0 {
		t.Fatalf("successful provider refunds %d %v", count, err)
	}
	credit, err := l.CreditBalance(ctx, correction.GrantIDs[0])
	if err != nil || credit.GrantedMinor != 1000 || credit.ReservedMinor != 0 || credit.RefundedMinor != 0 || credit.AvailableMinor != 1000 {
		t.Fatalf("credit after failed refund %+v %v", credit, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "decision-refund-001", "C48", refundID, json.RawMessage(`{"status":"definitively_failed"}`), "")
	if err != nil || !replay || replayed.ID != command.ID || replayed.Status != "succeeded" {
		t.Fatalf("replayed refund decision %+v replay=%t err=%v", replayed, replay, err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "decision-refund-001", "C48", refundID, json.RawMessage(`{"status":"succeeded"}`), ""); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed decision with original key: %v", err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("provider receipts after replay %d %v", count, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin receipts after replay %d %v", count, err)
	}
}

func TestAdminRefundDecisionRejectsInvalidPayloadBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{
		`{}`,
		`{"status":"pending"}`,
		`{"status":"SUCCEEDED"}`,
		`{"status":"succeeded","unexpected":true}`,
		`null`,
	} {
		if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "invalid-refund-decision", "C48", "refund_missing", json.RawMessage(payload), ""); !errors.Is(err, ErrAdminInvalidCommand) {
			t.Errorf("payload %s: got %v, want invalid command", payload, err)
		}
	}
	var count int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE action_id='C48'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid refund decisions admitted %d %v", count, err)
	}
}

func TestAdminPaymentDecisionRecoversAfterProviderCommitAndCapture(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	open := func() *Lab {
		t.Helper()
		l, err := Open(commerce, provider, func() time.Time { return fixedNow })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	quote, err := l.CreateQuote(ctx, "payment-control-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "payment-control-checkout")
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "payment-control-decision", "C47", accepted.OperationID, json.RawMessage(`{"status":"succeeded"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.adminSetDecision(ctx, command.ID, "payment", key, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("recovered payment decision %+v %v", command, err)
	}
	var count int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("provider control receipts %d %v", count, err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE provider_key=?`, key).Scan(&count); err != nil || count != 1 {
		t.Fatalf("captures %d %v", count, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin command receipts %d %v", count, err)
	}
}
