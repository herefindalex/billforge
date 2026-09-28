package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type adminBatchTarget struct {
	Kind        string
	ID          string
	PeriodKey   string
	PayloadHash string
	Usage       *usageCreditNotePreviewPeriod
	Maintenance *adminMaintenanceItem
	Change      *changeCorrectionPreviewItem
}

func adminBatchTargets(actionID, sourceJSON string) ([]adminBatchTarget, time.Time, error) {
	var targets []adminBatchTarget
	var previewAt time.Time
	switch actionID {
	case "C13":
		var source map[string]string
		if err := json.Unmarshal([]byte(sourceJSON), &source); err != nil {
			return nil, previewAt, err
		}
		var changes []changeCorrectionPreviewItem
		if err := json.Unmarshal([]byte(source["items_json"]), &changes); err != nil {
			return nil, previewAt, err
		}
		for i := range changes {
			item := &changes[i]
			encoded, _ := json.Marshal(item)
			targets = append(targets, adminBatchTarget{Kind: "immediate_change", ID: item.ChangeID, PayloadHash: hash(actionID, string(encoded)), Change: item})
		}
	case "C30":
		var source map[string]string
		if err := json.Unmarshal([]byte(sourceJSON), &source); err != nil {
			return nil, previewAt, err
		}
		var periods []usageCreditNotePreviewPeriod
		if err := json.Unmarshal([]byte(source["periods_json"]), &periods); err != nil {
			return nil, previewAt, err
		}
		for i := range periods {
			period := &periods[i]
			encoded, _ := json.Marshal(period)
			targets = append(targets, adminBatchTarget{Kind: "usage_period", ID: period.SubscriptionID, PeriodKey: strconv.Itoa(period.PeriodIndex), PayloadHash: hash(actionID, string(encoded)), Usage: period})
		}
	case "C32":
		var source struct {
			OperationIDs []string `json:"operation_ids"`
			AsOf         string   `json:"as_of"`
		}
		if err := json.Unmarshal([]byte(sourceJSON), &source); err != nil {
			return nil, previewAt, err
		}
		at, err := time.Parse(time.RFC3339Nano, source.AsOf)
		if err != nil {
			return nil, previewAt, err
		}
		previewAt = at
		for _, id := range source.OperationIDs {
			targets = append(targets, adminBatchTarget{Kind: "payment_operation", ID: id, PayloadHash: hash(actionID, id)})
		}
	case "C44", "C45":
		var source adminMaintenanceSource
		if err := json.Unmarshal([]byte(sourceJSON), &source); err != nil {
			return nil, previewAt, err
		}
		at, err := time.Parse(time.RFC3339Nano, source.AsOf)
		if err != nil {
			return nil, previewAt, err
		}
		previewAt = at
		for i := range source.Items {
			item := &source.Items[i]
			encoded, _ := json.Marshal(item)
			targets = append(targets, adminBatchTarget{Kind: "subscription", ID: item.SubscriptionID, PeriodKey: strconv.Itoa(item.PeriodIndex), PayloadHash: hash(actionID, string(encoded)), Maintenance: item})
		}
	default:
		return nil, previewAt, ErrAdminUnsupportedAction
	}
	if len(targets) == 0 || len(targets) > 100 {
		return nil, previewAt, ErrAdminPreviewStale
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target.ID == "" {
			return nil, previewAt, ErrAdminPreviewStale
		}
		key := target.Kind + ":" + target.ID + ":" + target.PeriodKey
		if seen[key] {
			return nil, previewAt, ErrAdminPreviewStale
		}
		seen[key] = true
	}
	return targets, previewAt, nil
}

func (l *Lab) adminFreezeBatch(ctx context.Context, commandID string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actionID, actorID, status, previewID, businessTime string
	if err := tx.QueryRowContext(ctx, `SELECT action_id,actor_id,status,COALESCE(preview_id,''),business_time FROM admin_commands WHERE id=?`, commandID).Scan(&actionID, &actorID, &status, &previewID, &businessTime); err != nil {
		return err
	}
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return err
	}
	if status != "accepted" {
		return tx.Commit()
	}
	var sourceJSON, expiry string
	err = tx.QueryRowContext(ctx, `SELECT source_versions_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id=? AND target_id='' AND claimed_command_id=?`, previewID, actorID, actionID, commandID).Scan(&sourceJSON, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAdminPreviewStale
	}
	if err != nil {
		return err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !time.Now().UTC().Before(expires) {
		return ErrAdminPreviewStale
	}
	targets, previewAt, err := adminBatchTargets(actionID, sourceJSON)
	if err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil || (!previewAt.IsZero() && at.Before(previewAt)) {
		return ErrAdminPreviewStale
	}
	jobID := "job:" + commandID
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_jobs(id,command_id,kind,status,created_at,updated_at) VALUES(?,?,?,'running',?,?)`, jobID, commandID, actionID, stamp, stamp); err != nil {
		return err
	}
	for _, target := range targets {
		id, err := newID("jobitem_")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_job_items(id,job_id,target_type,target_id,period_key,payload_hash,status,updated_at) VALUES(?,?,?,?,?,?,'accepted',?)`, id, jobID, target.Kind, target.ID, target.PeriodKey, target.PayloadHash, stamp); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET status='running',updated_at=? WHERE id=? AND status='accepted'`, stamp, commandID); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) adminExecuteBatchCommand(ctx context.Context, commandID string) (AdminCommand, error) {
	l.batchMu.Lock()
	defer l.batchMu.Unlock()
	var actionID, status, previewID, businessTime, actorID string
	if err := l.db.QueryRowContext(ctx, `SELECT action_id,status,COALESCE(preview_id,''),business_time,actor_id FROM admin_commands WHERE id=?`, commandID).Scan(&actionID, &status, &previewID, &businessTime, &actorID); err != nil {
		return AdminCommand{}, err
	}
	if status == "succeeded" || status == "failed" {
		return l.AdminCommand(ctx, commandID)
	}
	if status == "accepted" {
		if err := l.adminFreezeBatch(ctx, commandID); err != nil {
			if errors.Is(err, ErrAdminPreviewStale) {
				if failErr := l.adminFailCommand(ctx, commandID, actorID, actionID, "", "PREVIEW_STALE"); failErr != nil {
					return AdminCommand{}, failErr
				}
				return l.AdminCommand(ctx, commandID)
			}
			return AdminCommand{}, err
		}
	} else if status != "running" {
		return AdminCommand{}, ErrConflict
	}
	var sourceJSON string
	if err := l.db.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=?`, previewID).Scan(&sourceJSON); err != nil {
		return AdminCommand{}, err
	}
	targets, _, err := adminBatchTargets(actionID, sourceJSON)
	if err != nil {
		return AdminCommand{}, err
	}
	lookup := map[string]adminBatchTarget{}
	for _, target := range targets {
		lookup[target.Kind+":"+target.ID+":"+target.PeriodKey] = target
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id,target_type,target_id,period_key,payload_hash FROM admin_job_items WHERE job_id=? AND status='accepted' ORDER BY rowid`, "job:"+commandID)
	if err != nil {
		return AdminCommand{}, err
	}
	type pendingItem struct{ id, kind, targetID, periodKey, payloadHash string }
	var pending []pendingItem
	for rows.Next() {
		var item pendingItem
		if err := rows.Scan(&item.id, &item.kind, &item.targetID, &item.periodKey, &item.payloadHash); err != nil {
			rows.Close()
			return AdminCommand{}, err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminCommand{}, err
	}
	rows.Close()
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return AdminCommand{}, err
	}
	for _, item := range pending {
		if err := l.adminRenewLease(ctx, commandID); err != nil {
			return AdminCommand{}, err
		}
		target, ok := lookup[item.kind+":"+item.targetID+":"+item.periodKey]
		if !ok || target.PayloadHash != item.payloadHash {
			return AdminCommand{}, ErrConflict
		}
		if err := l.adminRunBatchItem(ctx, commandID, actionID, at, item.id, target); err != nil {
			if errors.Is(err, ErrAdminPreviewStale) || errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
				if err := l.adminMarkBatchConflict(ctx, commandID, item.id); err != nil {
					return AdminCommand{}, err
				}
				continue
			}
			return AdminCommand{}, err
		}
	}
	if err := l.adminRenewLease(ctx, commandID); err != nil {
		return AdminCommand{}, err
	}
	return l.adminFinishBatch(ctx, commandID, actionID, actorID)
}

func (l *Lab) adminRunBatchItem(ctx context.Context, commandID, actionID string, at time.Time, itemID string, target adminBatchTarget) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM admin_job_items WHERE id=? AND job_id=?`, itemID, "job:"+commandID).Scan(&status); err != nil {
		return err
	}
	if status != "accepted" {
		return tx.Commit()
	}
	refs := map[string]any{"target_id": target.ID}
	itemStatus := "succeeded"
	switch actionID {
	case "C13":
		expected := target.Change
		current, err := l.loadChangeCorrectionPreviewItem(ctx, tx, expected.ChangeID)
		if err != nil {
			return err
		}
		if current != *expected {
			return ErrAdminPreviewStale
		}
		correction, err := l.runChangeCorrectionTx(ctx, tx, at, expected.ChangeID)
		if err != nil {
			return err
		}
		if correction.InvoiceID != expected.InvoiceID || strconv.FormatInt(correction.ReductionMinor, 10) != expected.ReductionMinor {
			return ErrAdminPreviewStale
		}
		refs["correction_id"], refs["invoice_id"], refs["reduction_minor"], refs["grant_ids"] = correction.ID, correction.InvoiceID, expected.ReductionMinor, correction.GrantIDs
	case "C30":
		expected := target.Usage
		current, err := l.loadUsageCreditNotePreviewPeriod(ctx, tx, expected.SubscriptionID, expected.PeriodIndex)
		if err != nil {
			return err
		}
		actualJSON, _ := json.Marshal(current)
		expectedJSON, _ := json.Marshal(expected)
		if string(actualJSON) != string(expectedJSON) {
			return ErrAdminPreviewStale
		}
		var corrections []map[string]any
		for _, sourceItem := range expected.Items {
			correction, applied, err := l.runUsageCreditNoteTx(ctx, tx, at, expected.SubscriptionID, expected.PeriodIndex)
			if err != nil {
				return err
			}
			if !applied || correction.InvoiceID != sourceItem.InvoiceID || strconv.FormatInt(correction.ReductionMinor, 10) != sourceItem.AmountMinor {
				return ErrAdminPreviewStale
			}
			corrections = append(corrections, map[string]any{"invoice_id": correction.InvoiceID, "correction_id": correction.ID, "amount_minor": sourceItem.AmountMinor, "grant_ids": correction.GrantIDs})
		}
		refs["period_index"], refs["corrections"] = target.PeriodKey, corrections
	case "C32":
		result, err := tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status)
			SELECT 'capture:'||o.id,'capture',o.id,'pending' FROM payment_operations o
			JOIN billing_periods p ON p.invoice_id=o.invoice_id
			JOIN invoice_contracts c ON c.invoice_id=o.invoice_id
			WHERE o.id=? AND o.status='created' AND p.due_at<=? AND NOT EXISTS(SELECT 1 FROM outbox b WHERE b.id='capture:'||o.id)`, target.ID, at.UTC().UnixNano())
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrAdminPreviewStale
		}
		refs["outbox_id"] = "capture:" + target.ID
	case "C44", "C45":
		expected := target.Maintenance
		var currentStatus string
		var currentRevision int64
		var currentPeriod int
		err := tx.QueryRowContext(ctx, `SELECT s.status,s.revision,COALESCE((SELECT MAX(p.period_index) FROM billing_periods p WHERE p.subscription_id=s.id),-1) FROM subscriptions s WHERE s.id=?`, target.ID).Scan(&currentStatus, &currentRevision, &currentPeriod)
		if err != nil {
			return err
		}
		if currentStatus != "active" || currentRevision != expected.Revision || currentPeriod != expected.PeriodIndex {
			return ErrAdminPreviewStale
		}
		if actionID == "C44" {
			receipt, made, err := l.renewOneTx(ctx, tx, at, target.ID)
			if err != nil {
				return err
			}
			if made {
				refs["invoice_id"] = receipt.InvoiceID
				refs["amount_minor"] = strconv.FormatInt(receipt.AmountMinor, 10)
			} else {
				itemStatus = "skipped"
			}
		} else if err := l.refreshOneEntitlementTx(ctx, tx, at, target.ID); err != nil {
			return err
		}
		refs["period_index"] = target.PeriodKey
	default:
		return ErrAdminUnsupportedAction
	}
	encoded, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE admin_job_items SET status=?,result_refs_json=?,updated_at=? WHERE id=? AND status='accepted'`, itemStatus, string(encoded), stamp, itemID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) adminMarkBatchConflict(ctx context.Context, commandID, itemID string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_job_items SET status='conflicted',error_code='SOURCE_CHANGED',updated_at=? WHERE id=? AND status='accepted'`, time.Now().UTC().Format(time.RFC3339Nano), itemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) adminFinishBatch(ctx context.Context, commandID, actionID, actorID string) (AdminCommand, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminCommand{}, err
	}
	defer tx.Rollback()
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return AdminCommand{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT target_type,target_id,period_key,status,COALESCE(result_refs_json,''),COALESCE(error_code,'') FROM admin_job_items WHERE job_id=? ORDER BY rowid`, "job:"+commandID)
	if err != nil {
		return AdminCommand{}, err
	}
	var items []map[string]any
	successes, failures, conflicts, waiting, skipped := 0, 0, 0, 0, 0
	for rows.Next() {
		var kind, id, period, status, raw, code string
		if err := rows.Scan(&kind, &id, &period, &status, &raw, &code); err != nil {
			rows.Close()
			return AdminCommand{}, err
		}
		if status == "accepted" || status == "running" {
			rows.Close()
			return AdminCommand{}, fmt.Errorf("unfinished batch item %s", id)
		}
		switch status {
		case "succeeded":
			successes++
		case "failed":
			failures++
		case "conflicted":
			conflicts++
		case "waiting_verification":
			waiting++
		case "skipped":
			skipped++
		default:
			rows.Close()
			return AdminCommand{}, fmt.Errorf("unexpected batch item status %q", status)
		}
		item := map[string]any{"target_type": kind, "target_id": id, "period_key": period, "status": status}
		if raw != "" {
			item["result_refs"] = json.RawMessage(raw)
		}
		if code != "" {
			item["error_code"] = code
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminCommand{}, err
	}
	rows.Close()
	jobStatus := "succeeded"
	if failures > 0 || conflicts > 0 || waiting > 0 || skipped > 0 {
		jobStatus = "partial"
	}
	refs := map[string]any{"job_id": "job:" + commandID, "status": jobStatus, "item_count": strconv.Itoa(len(items)), "succeeded_count": strconv.Itoa(successes), "failed_count": strconv.Itoa(failures), "conflicted_count": strconv.Itoa(conflicts), "waiting_verification_count": strconv.Itoa(waiting), "skipped_count": strconv.Itoa(skipped), "items": items}
	encoded, err := json.Marshal(refs)
	if err != nil {
		return AdminCommand{}, err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE admin_jobs SET status=?,updated_at=? WHERE command_id=?`, jobStatus, stamp, commandID); err != nil {
		return AdminCommand{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_command_receipts(command_id,domain_request_key,result_refs_json,committed_at) VALUES(?,?,?,?)`, commandID, "admin:"+commandID, string(encoded), stamp); err != nil {
		return AdminCommand{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET status='succeeded',result_refs_json=?,updated_at=? WHERE id=? AND status='running'`, string(encoded), stamp, commandID); err != nil {
		return AdminCommand{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,after_json,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?,?)`, commandID, adminRequestID(ctx), commandID, actorID, actionID, "", jobStatus, string(encoded), stamp); err != nil {
		return AdminCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminCommand{}, err
	}
	return l.AdminCommand(ctx, commandID)
}
