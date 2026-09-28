package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminPublishContractReceiptFailureRecoversWithoutDuplicateVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := func() time.Time { return fixedNow }
	l, err := Open(commerce, provider, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(adminPublishContractPayload{
		ID: "receipt-contract-v1", CustomerID: "receipt-contract-customer", Version: "1",
		BasePriceVersionID: "pro-v1", FixedMinor: "4000", SeatMinor: "700",
		EffectiveFrom: fixedNow.Format(time.RFC3339Nano),
		EffectiveTo:   fixedNow.Add(30 * 24 * time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C31", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "receipt-contract-key", "C31", "", payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit: %+v replay=%t err=%v", command, replay, err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_contract_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure should roll back published contract")
	}
	var versions, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_versions WHERE id='receipt-contract-v1'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if versions != 0 || receipts != 0 {
		t.Fatalf("receipt failure left partial publication: versions=%d receipts=%d", versions, receipts)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := l.AdminCommand(ctx, command.ID)
	if err != nil || recovered.Status != "succeeded" {
		t.Fatalf("original command not recovered: %+v err=%v", recovered, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "receipt-contract-key", "C31", "", payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("idempotent replay changed command: %+v replay=%t err=%v", replayed, replay, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_versions WHERE id='receipt-contract-v1'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || receipts != 1 {
		t.Fatalf("recovery duplicated or lost publication: versions=%d receipts=%d", versions, receipts)
	}
}

func TestAdminPublishContractPreviewConcurrentSameID(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "same_content"
		if changed {
			name = "different_content"
		}
		t.Run(name, func(t *testing.T) {
			l, _ := openChangingLab(t)
			ctx := context.Background()
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			spec := ContractSpec{
				ID: "concurrent-contract", CustomerID: "concurrent-customer", Version: 1,
				BasePriceVersionID: "pro-v1", FixedMinor: 4000, SeatMinor: 700,
				EffectiveFrom: fixedNow, EffectiveTo: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			}
			payload, err := json.Marshal(adminPublishContractPayload{
				ID: spec.ID, CustomerID: spec.CustomerID, Version: "1",
				BasePriceVersionID: spec.BasePriceVersionID, FixedMinor: "4000", SeatMinor: "700",
				EffectiveFrom: spec.EffectiveFrom.Format(time.RFC3339Nano),
				EffectiveTo:   spec.EffectiveTo.Format(time.RFC3339Nano),
			})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := l.AdminCreatePreview(ctx, "local-admin", "C31", "", payload)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				spec.FixedMinor = 5000
			}
			concurrent, err := l.PublishContract(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "concurrent-contract-key", "C31", "", payload, preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			command, err = l.AdminExecuteCommand(ctx, command.ID)
			if err != nil {
				t.Fatal(err)
			}
			var fixedMinor int64
			var checksum string
			if err := l.db.QueryRowContext(ctx, `SELECT fixed_minor,checksum FROM contract_versions WHERE id=?`, spec.ID).Scan(&fixedMinor, &checksum); err != nil {
				t.Fatal(err)
			}
			if fixedMinor != spec.FixedMinor || checksum != concurrent.Checksum {
				t.Fatalf("concurrent contract changed: fixed=%d checksum=%s", fixedMinor, checksum)
			}
			var contracts, receipts int
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_versions WHERE id=?`, spec.ID).Scan(&contracts); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if changed {
				if command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" || contracts != 1 || receipts != 0 {
					t.Fatalf("changed contract command=%+v contracts=%d receipts=%d", command, contracts, receipts)
				}
				if _, err := l.AdminCreatePreview(ctx, "local-admin", "C31", "", payload); !errors.Is(err, ErrConflict) {
					t.Fatalf("old terms accepted after conflicting publication: %v", err)
				}
			} else if command.Status != "succeeded" || contracts != 1 || receipts != 1 {
				t.Fatalf("matching contract command=%+v contracts=%d receipts=%d", command, contracts, receipts)
			}
			replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "concurrent-contract-key", "C31", "", payload, preview.ID)
			if err != nil || !replay || replayed.ID != command.ID {
				t.Fatalf("replay=%+v replayed=%v err=%v", replayed, replay, err)
			}
		})
	}
}

func TestAdminContractCollectionPreviewConflictsWhenOperationQueuedElsewhere(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	spec := acmeContract("")
	if _, err := l.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := l.AcceptContractQuote(ctx, quote.ID, quote.Fingerprint, "contract-external-queue")
	if err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := l.CollectDueContractInvoices(ctx)
	if err != nil || queued != 1 {
		t.Fatalf("other collector queued=%d err=%v", queued, err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "contract-already-queued", "C32", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("batch command=%+v err=%v", command, err)
	}
	var itemStatus, itemCode string
	if err := l.db.QueryRowContext(ctx, `SELECT status,error_code FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, receipt.OperationID).Scan(&itemStatus, &itemCode); err != nil {
		t.Fatal(err)
	}
	var outbox, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=?`, "capture:"+receipt.OperationID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if itemStatus != "conflicted" || itemCode != "SOURCE_CHANGED" || outbox != 1 || receipts != 1 {
		t.Fatalf("item=%s/%s outbox=%d receipts=%d", itemStatus, itemCode, outbox, receipts)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "contract-already-queued", "C32", "", payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("replayed=%+v replay=%v err=%v", replayed, replay, err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", payload); !errors.Is(err, ErrConflict) {
		t.Fatalf("already queued operation appeared in new collection preview: %v", err)
	}
}

func TestContractQuoteCommandRequiresExclusiveSourceAndPositiveSeats(t *testing.T) {
	valid := `{"customer_id":"acme","contract_version_id":"contract-v1","seats":"5"}`
	if _, err := canonicalAdminPayload("C01", json.RawMessage(valid)); err != nil {
		t.Fatalf("valid contract quote rejected: %v", err)
	}
	for _, payload := range []string{
		`{"customer_id":"acme","contract_version_id":"contract-v1","plan_id":"pro","seats":"5"}`,
		`{"customer_id":"acme","contract_version_id":"contract-v1","cohort":"default","seats":"5"}`,
		`{"customer_id":"acme","contract_version_id":"contract-v1","seats":"0"}`,
		`{"customer_id":"acme","contract_version_id":"contract-v1","seats":"5","change_subscription_id":"sub-1","mode":"next_period","revision":"1"}`,
	} {
		if _, err := canonicalAdminPayload("C01", json.RawMessage(payload)); err == nil {
			t.Fatalf("invalid contract quote accepted: %s", payload)
		}
	}
}

func TestAdminPublishContractPreviewAndAtomicReceipt(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(adminPublishContractPayload{
		ID: "admin-contract-v1", CustomerID: "admin-customer", Version: "1",
		BasePriceVersionID: "pro-v1", FixedMinor: "4000", SeatMinor: "700",
		EffectiveFrom: fixedNow.UTC().Format(time.RFC3339Nano),
		EffectiveTo:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C31", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_versions WHERE id='admin-contract-v1'`).Scan(&before); err != nil || before != 0 {
		t.Fatalf("preview changed contract state: count=%d err=%v", before, err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "publish-contract-001", "C31", "", payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit: %+v replay=%v err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var contracts, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_versions WHERE id='admin-contract-v1'`).Scan(&contracts); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if contracts != 1 || receipts != 1 {
		t.Fatalf("contracts=%d receipts=%d", contracts, receipts)
	}
	if _, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "publish-contract-001", "C31", "", payload, preview.ID); err != nil || !replay {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
}

func TestAdminCollectDueContractInvoicesUsesPreviewedOperations(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	spec := acmeContract("")
	spec.EffectiveTo = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := l.AcceptContractQuote(ctx, quote.ID, quote.Fingerprint, "admin-collection-accept")
	if err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	newQuote, err := l.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	newReceipt, err := l.AcceptContractQuote(ctx, newQuote.ID, newQuote.Fingerprint, "admin-collection-later-accept")
	if err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "collect-contract-001", "C32", "", json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var outbox, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id='capture:'||?`, receipt.OperationID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if outbox != 1 || receipts != 1 {
		t.Fatalf("outbox=%d receipts=%d", outbox, receipts)
	}
	var laterOutbox int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id='capture:'||?`, newReceipt.OperationID).Scan(&laterOutbox); err != nil || laterOutbox != 0 {
		t.Fatalf("newly eligible operation joined fixed batch: count=%d err=%v", laterOutbox, err)
	}
	next, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]json.RawMessage
	if err := json.Unmarshal(next.Impact, &impact); err != nil || string(impact["count"]) != `"1"` {
		t.Fatalf("new collection preview: %s err=%v", next.Impact, err)
	}
	var nextIDs []string
	if err := json.Unmarshal(impact["operation_ids"], &nextIDs); err != nil || len(nextIDs) != 1 || nextIDs[0] != newReceipt.OperationID {
		t.Fatalf("next preview did not select newly due operation: %+v err=%v", nextIDs, err)
	}
}

func TestAdminContractCollectionPreviewReportsBacklogBeyondHundred(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	spec := acmeContract("")
	if _, err := l.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		quote, err := l.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.AcceptContractQuote(ctx, quote.ID, quote.Fingerprint, fmt.Sprintf("contract-backlog-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]json.RawMessage
	if err := json.Unmarshal(preview.Impact, &impact); err != nil || string(impact["has_more_candidates"]) != `"true"` {
		t.Fatalf("collection preview hid backlog: %s, %v", preview.Impact, err)
	}
	var firstIDs []string
	if err := json.Unmarshal(impact["operation_ids"], &firstIDs); err != nil || len(firstIDs) != 100 {
		t.Fatalf("first collection membership: count=%d err=%v", len(firstIDs), err)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rotated, more, err := dueContractOperations(ctx, tx, *clock, firstIDs[len(firstIDs)-1])
	_ = tx.Rollback()
	firstSet := make(map[string]bool, len(firstIDs))
	for _, id := range firstIDs {
		firstSet[id] = true
	}
	if err != nil || !more || len(rotated) != 100 || firstSet[rotated[0]] {
		t.Fatalf("collection cursor did not advance: first=%s rotated=%v more=%v err=%v", firstIDs[0], rotated, more, err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "contract-backlog-first", "C32", "", json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("first contract batch: %+v, %v", command, err)
	}
	var firstOutbox int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id LIKE 'capture:%'`).Scan(&firstOutbox); err != nil || firstOutbox != 100 {
		t.Fatalf("first contract batch outbox count: %d, %v", firstOutbox, err)
	}
	next, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(next.Impact, &impact); err != nil || string(impact["count"]) != `"1"` || string(impact["has_more_candidates"]) != `"false"` {
		t.Fatalf("remaining contract batch: %s, %v", next.Impact, err)
	}
}
