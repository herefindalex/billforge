package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type applyCreditSnapshot struct {
	Currency string
	Sources  []byte
	Impact   []byte
}

func (l *Lab) loadApplyCreditSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, grantID, invoiceID string, amountMinor int64) (applyCreditSnapshot, error) {
	var snapshot applyCreditSnapshot
	var sourceInvoice, sourceCustomer, targetCustomer string
	if err := tx.QueryRowContext(ctx, `SELECT g.source_invoice_id,s.customer_id FROM credit_grants g JOIN invoices i ON i.id=g.source_invoice_id JOIN subscriptions s ON s.id=i.subscription_id WHERE g.id=?`, grantID).Scan(&sourceInvoice, &sourceCustomer); err != nil {
		return snapshot, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT s.customer_id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE i.id=?`, invoiceID).Scan(&targetCustomer); err != nil {
		return snapshot, err
	}
	if sourceInvoice == invoiceID || sourceCustomer != targetCustomer {
		return snapshot, ErrConflict
	}
	var periodIndex int
	var dueAt, end int64
	if err := tx.QueryRowContext(ctx, `SELECT period_index,due_at,period_end FROM billing_periods WHERE invoice_id=?`, invoiceID).Scan(&periodIndex, &dueAt, &end); err != nil {
		return snapshot, err
	}
	if periodIndex == 0 || at.UnixNano() >= end {
		return snapshot, ErrConflict
	}
	if at.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
		return snapshot, ErrLateNeedsReview
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND status IN ('submitted','unknown')`, invoiceID).Scan(&active); err != nil {
		return snapshot, err
	}
	if active != 0 {
		return snapshot, ErrConflict
	}
	grant, err := loadCreditBalance(ctx, tx, grantID)
	if err != nil {
		return snapshot, err
	}
	invoice, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return snapshot, err
	}
	if amountMinor <= 0 || grant.Currency != invoice.Currency || amountMinor > grant.AvailableMinor || amountMinor > invoice.OutstandingMinor {
		return snapshot, ErrConflict
	}
	var operationStates string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM payment_operations WHERE invoice_id=? ORDER BY id)`, invoiceID).Scan(&operationStates); err != nil {
		return snapshot, err
	}
	var createdPayments int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND status='created'`, invoiceID).Scan(&createdPayments); err != nil {
		return snapshot, err
	}
	snapshot.Currency = grant.Currency
	snapshot.Sources, err = json.Marshal(map[string]string{
		"grant_id": grantID, "source_invoice_id": sourceInvoice, "customer_id": sourceCustomer,
		"grant_available_minor": strconv.FormatInt(grant.AvailableMinor, 10),
		"grant_applied_minor":   strconv.FormatInt(grant.AppliedMinor, 10),
		"grant_reserved_minor":  strconv.FormatInt(grant.ReservedMinor, 10),
		"grant_refunded_minor":  strconv.FormatInt(grant.RefundedMinor, 10),
		"invoice_id":            invoiceID, "invoice_outstanding_minor": strconv.FormatInt(invoice.OutstandingMinor, 10),
		"period_index": strconv.Itoa(periodIndex), "due_at": strconv.FormatInt(dueAt, 10),
		"period_end": strconv.FormatInt(end, 10), "operation_states": operationStates, "currency": grant.Currency,
	})
	if err != nil {
		return applyCreditSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(map[string]string{
		"grant_id": grantID, "invoice_id": invoiceID, "amount_minor": strconv.FormatInt(amountMinor, 10),
		"grant_available_before_minor":     strconv.FormatInt(grant.AvailableMinor, 10),
		"invoice_outstanding_before_minor": strconv.FormatInt(invoice.OutstandingMinor, 10),
		"invoice_outstanding_after_minor":  strconv.FormatInt(invoice.OutstandingMinor-amountMinor, 10),
		"created_payments_to_cancel":       strconv.Itoa(createdPayments), "currency": grant.Currency,
	})
	return snapshot, err
}

func (l *Lab) adminCreateApplyCreditPreview(ctx context.Context, actorID, grantID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || grantID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var input AdminApplyCreditPayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	amount, err := strconv.ParseInt(input.AmountMinor, 10, 64)
	if err != nil {
		return AdminPreview{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadApplyCreditSnapshot(ctx, tx, l.now().UTC(), grantID, input.InvoiceID, amount)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C12", grantID, adminIntentHash("C12", grantID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C12", TargetID: grantID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeApplyCreditTx(ctx context.Context, tx *sql.Tx, commandID, previewID, grantID, payloadJSON, businessTime string) (any, error) {
	var input AdminApplyCreditPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
	amount, err := strconv.ParseInt(input.AmountMinor, 10, 64)
	if err != nil {
		return nil, err
	}
	var storedSources string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	snapshot, err := l.loadApplyCreditSnapshot(ctx, tx, at, grantID, input.InvoiceID, amount)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrLateNeedsReview) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	id, err := l.applyCreditTx(ctx, tx, at, grantID, input.InvoiceID, amount, "admin:"+commandID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrLateNeedsReview) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"application_id": id, "grant_id": grantID, "invoice_id": input.InvoiceID, "amount_minor": input.AmountMinor, "currency": snapshot.Currency}, nil
}
