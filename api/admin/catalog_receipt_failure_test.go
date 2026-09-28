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

func TestCatalogPublishReceiptFailureRecoversOriginalHTTPCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	l, err := lab.Open(commercePath, filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const priceID = "pro-catalog-receipt-fault-v2"
	fields := map[string]string{
		"id": priceID, "version": "2", "fixed_minor": "6000", "seat_minor": "1000",
		"included_tasks": "100", "usage_rate_num": "1", "usage_rate_den": "1",
		"effective_from": time.Now().UTC().Add(90 * 24 * time.Hour).Format(time.RFC3339Nano),
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C18", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inspection.Close() })
	if _, err := inspection.Exec(`CREATE TRIGGER fail_catalog_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}

	const token = "catalog-receipt-fault-session"
	const csrf = "catalog-receipt-fault-csrf"
	const requestKey = "catalog-receipt-fault-key"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {csrf: csrf, capabilities: []string{"read", "catalog.publish"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/commands", s.protectedWrite(s.submitCommand))
	post := func(input json.RawMessage) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"action_id": "C18", "target_id": "", "preview_id": preview.ID, "payload": input})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", requestKey)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	count := func(query string, args ...any) int64 {
		t.Helper()
		var result int64
		if err := inspection.QueryRow(query, args...).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := post(payload)
	if first.Code != http.StatusServiceUnavailable || !strings.Contains(first.Body.String(), `"code":"COMMAND_PENDING_RETRY"`) {
		t.Fatalf("first C18 status=%d body=%s", first.Code, first.Body.String())
	}
	var pending struct {
		CommandID string `json:"command_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &pending); err != nil || pending.CommandID == "" {
		t.Fatalf("pending command=%+v error=%v", pending, err)
	}
	if count(`SELECT COUNT(*) FROM admin_commands WHERE id=? AND idempotency_key=?`, pending.CommandID, requestKey) != 1 ||
		count(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID) != 0 ||
		count(`SELECT COUNT(*) FROM price_versions WHERE id=?`, priceID) != 0 ||
		count(`SELECT COUNT(*) FROM price_components WHERE price_version_id=?`, priceID) != 0 {
		t.Fatal("receipt failure left a partial catalog publication")
	}

	if _, err := inspection.Exec(`DROP TRIGGER fail_catalog_receipt`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w := post(payload)
		if w.Code != http.StatusOK {
			t.Fatalf("replay %d status=%d body=%s", i, w.Code, w.Body.String())
		}
		var command lab.AdminCommand
		if err := json.Unmarshal(w.Body.Bytes(), &command); err != nil || command.ID != pending.CommandID || command.Status != "succeeded" {
			t.Fatalf("replay %d command=%+v error=%v", i, command, err)
		}
	}
	if count(`SELECT COUNT(*) FROM admin_commands WHERE id=?`, pending.CommandID) != 1 ||
		count(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID) != 1 ||
		count(`SELECT COUNT(*) FROM price_versions WHERE id=?`, priceID) != 1 ||
		count(`SELECT COUNT(*) FROM price_components WHERE price_version_id=?`, priceID) != 3 ||
		count(`SELECT COUNT(*) FROM payment_operations`) != 0 {
		t.Fatal("recovery duplicated catalog or financial facts")
	}
	var priceChecksum, receiptJSON string
	if err := inspection.QueryRow(`SELECT checksum FROM price_versions WHERE id=?`, priceID).Scan(&priceChecksum); err != nil {
		t.Fatal(err)
	}
	if err := inspection.QueryRow(`SELECT result_refs_json FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&receiptJSON); err != nil {
		t.Fatal(err)
	}
	var refs map[string]string
	if err := json.Unmarshal([]byte(receiptJSON), &refs); err != nil || refs["price_version_id"] != priceID || refs["checksum"] != priceChecksum {
		t.Fatalf("receipt refs=%v checksum=%s error=%v", refs, priceChecksum, err)
	}
	fields["fixed_minor"] = "7000"
	changed, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	w := post(changed)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"IDEMPOTENCY_CONFLICT"`) {
		t.Fatalf("changed payload status=%d body=%s", w.Code, w.Body.String())
	}
}
