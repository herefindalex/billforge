package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestPaidReductionReceiptFailureRecoversOriginalHTTPCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := lab.Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "paid-reduction-http-receipt", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "paid-reduction-http-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	intent := json.RawMessage(`{"reduction_minor":"1000","reason":"service correction"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C11", paid.InvoiceID, intent)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	provider, err := sql.Open("sqlite3", providerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	if _, err := inspection.Exec(`CREATE TRIGGER fail_reduction_admin_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}

	const token = "reduction-receipt-fault-session"
	const csrf = "reduction-receipt-fault-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
			capabilities: []string{"read", "finance.adjust"},
		}},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/commands", s.protectedWrite(s.submitCommand))
	post := func(previewID string, payload json.RawMessage) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"action_id": "C11", "target_id": paid.InvoiceID, "preview_id": previewID,
			"payload": payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "paid-reduction-http-receipt-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	first := post(preview.ID, intent)
	var pending struct {
		CommandID string `json:"command_id"`
		Error     struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &pending); err != nil || first.Code != http.StatusServiceUnavailable || pending.Error.Code != "COMMAND_PENDING_RETRY" || !pending.Error.Retryable || pending.CommandID == "" {
		t.Fatalf("receipt failure status=%d body=%s decode=%v", first.Code, first.Body.String(), err)
	}
	checkFacts := func(reduction, grant, release, receipts int64) {
		t.Helper()
		balance, err := l.Balance(ctx, paid.InvoiceID)
		if err != nil {
			t.Fatal(err)
		}
		if balance.OriginalMinor != quote.AmountMinor || balance.ObligationMinor != quote.AmountMinor-reduction || balance.ReductionsMinor != reduction || balance.GrossCapturedMinor != quote.AmountMinor || balance.ReleasedMinor != release || balance.NetAppliedMinor != quote.AmountMinor-release || balance.OutstandingMinor != 0 {
			t.Fatalf("invoice balance=%+v reduction=%d release=%d", balance, reduction, release)
		}
		for _, item := range []struct {
			name  string
			query string
			args  []any
			want  int64
		}{
			{"corrections", `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, []any{paid.InvoiceID}, reduction / 1000},
			{"credit_grants", `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, []any{paid.InvoiceID}, grant / 1000},
			{"releases", `SELECT COUNT(*) FROM allocation_releases r JOIN allocations a ON a.operation_id=r.operation_id WHERE a.invoice_id=?`, []any{paid.InvoiceID}, release / 1000},
			{"receipts", `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, []any{pending.CommandID}, receipts},
			{"commands", `SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='paid-reduction-http-receipt-key'`, nil, 1},
		} {
			var got int64
			if err := inspection.QueryRow(item.query, item.args...).Scan(&got); err != nil || got != item.want {
				t.Fatalf("%s count=%d want=%d err=%v", item.name, got, item.want, err)
			}
		}
		var correctionSum, grantSum, releaseSum, invoiceTotal, lineTotal, captures int64
		if err := inspection.QueryRow(`SELECT COALESCE(SUM(reduction_minor),0) FROM corrections WHERE invoice_id=?`, paid.InvoiceID).Scan(&correctionSum); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COALESCE(SUM(amount_minor),0) FROM credit_grants WHERE source_invoice_id=?`, paid.InvoiceID).Scan(&grantSum); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COALESCE(SUM(r.amount_minor),0) FROM allocation_releases r JOIN allocations a ON a.operation_id=r.operation_id WHERE a.invoice_id=?`, paid.InvoiceID).Scan(&releaseSum); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT total_minor FROM invoices WHERE id=?`, paid.InvoiceID).Scan(&invoiceTotal); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COALESCE(SUM(amount_minor),0) FROM invoice_lines WHERE invoice_id=?`, paid.InvoiceID).Scan(&lineTotal); err != nil {
			t.Fatal(err)
		}
		if err := provider.QueryRow(`SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
			t.Fatal(err)
		}
		if correctionSum != reduction || grantSum != grant || releaseSum != release || invoiceTotal != quote.AmountMinor || lineTotal != quote.AmountMinor || captures != 1 {
			t.Fatalf("correction=%d grant=%d release=%d invoice=%d lines=%d captures=%d", correctionSum, grantSum, releaseSum, invoiceTotal, lineTotal, captures)
		}
		if receipts == 1 {
			var refsJSON string
			if err := inspection.QueryRow(`SELECT result_refs_json FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&refsJSON); err != nil {
				t.Fatal(err)
			}
			var refs struct {
				CorrectionID   string   `json:"correction_id"`
				InvoiceID      string   `json:"invoice_id"`
				ReductionMinor string   `json:"reduction_minor"`
				GrantIDs       []string `json:"grant_ids"`
			}
			if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil || refs.CorrectionID == "" || refs.InvoiceID != paid.InvoiceID || refs.ReductionMinor != "1000" || len(refs.GrantIDs) != 1 {
				t.Fatalf("receipt refs=%+v decode=%v", refs, err)
			}
			var correctionID, grantID string
			if err := inspection.QueryRow(`SELECT id FROM corrections WHERE invoice_id=?`, paid.InvoiceID).Scan(&correctionID); err != nil {
				t.Fatal(err)
			}
			if err := inspection.QueryRow(`SELECT id FROM credit_grants WHERE source_invoice_id=?`, paid.InvoiceID).Scan(&grantID); err != nil {
				t.Fatal(err)
			}
			if refs.CorrectionID != correctionID || refs.GrantIDs[0] != grantID {
				t.Fatalf("receipt refs=%+v differ from committed correction=%s grant=%s", refs, correctionID, grantID)
			}
		}
	}
	checkFacts(0, 0, 0, 0)
	if _, err := inspection.Exec(`DROP TRIGGER fail_reduction_admin_receipt`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		recovered := post(preview.ID, intent)
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(recovered.Body.Bytes(), &command); err != nil || recovered.Code != http.StatusOK || command.ID != pending.CommandID || command.Status != "succeeded" {
			t.Fatalf("replay %d status=%d body=%s decode=%v", i, recovered.Code, recovered.Body.String(), err)
		}
		checkFacts(1000, 1000, 1000, 1)
	}
	otherIntent := json.RawMessage(`{"reduction_minor":"500","reason":"another correction"}`)
	otherPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C11", paid.InvoiceID, otherIntent)
	if err != nil {
		t.Fatal(err)
	}
	conflict := post(otherPreview.ID, otherIntent)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_CONFLICT"`) {
		t.Fatalf("same key with another valid reduction status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	checkFacts(1000, 1000, 1000, 1)
}
