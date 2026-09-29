package lab

import (
	"context"
	"database/sql"
	"errors"
)

func loadRepairOperation(ctx context.Context, q rowQuerier, key string) (RepairOperation, error) {
	var x RepairOperation
	err := q.QueryRowContext(ctx, `SELECT id,discrepancy_id,action,expected,actual,precondition_revision,status,verification FROM repair_operations WHERE request_key=?`, key).Scan(&x.ID, &x.DiscrepancyID, &x.Action, &x.Expected, &x.Actual, &x.PreconditionRevision, &x.Status, &x.Verification)
	return x, err
}

func repairAction(d Discrepancy) string {
	if d.Classification == "SAFE_AUTO_REPAIR" && (d.Kind == "entitlement_projection" || d.Kind == "pending_outbox") {
		return "rebuild_entitlement"
	}
	if d.Classification == "RETRY_REQUIRED" && d.Kind == "pending_outbox" {
		return "retry_original_capture"
	}
	if d.Classification == "EXTERNAL_LOOKUP_REQUIRED" {
		return "lookup_original_operation"
	}
	return "manual_review"
}

func (l *Lab) RepairDiscrepancy(ctx context.Context, discrepancyID, requestKey string) (RepairOperation, error) {
	if discrepancyID == "" || requestKey == "" {
		return RepairOperation{}, ErrConflict
	}
	var x RepairOperation
	if saved, err := loadRepairOperation(ctx, l.db, requestKey); err == nil {
		if saved.DiscrepancyID != discrepancyID {
			return RepairOperation{}, ErrConflict
		}
		if saved.Status != "planned" && !(saved.Status == "waiting" && saved.Action == "lookup_original_operation") {
			return saved, nil
		}
		x = saved
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RepairOperation{}, err
	}
	d, err := loadDiscrepancy(ctx, l.db, discrepancyID)
	if err != nil {
		return RepairOperation{}, err
	}
	if d.Status == "resolved" && x.ID == "" {
		return RepairOperation{}, ErrConflict
	}
	if x.ID == "" {
		x = RepairOperation{DiscrepancyID: d.ID, Action: repairAction(d), Expected: d.Expected, Actual: d.Actual, PreconditionRevision: d.SourceRevision, Status: "planned"}
		x.ID, err = newID("repair_")
		if err != nil {
			return RepairOperation{}, err
		}
		if _, err := l.db.ExecContext(ctx, `INSERT INTO repair_operations(id,request_key,discrepancy_id,action,expected,actual,precondition_revision,status,created_at) VALUES(?,?,?,?,?,?,?,'planned',?)`, x.ID, requestKey, x.DiscrepancyID, x.Action, x.Expected, x.Actual, x.PreconditionRevision, l.now().UTC().UnixNano()); err != nil {
			return RepairOperation{}, err
		}
	}
	if x.Action == "manual_review" {
		x.Status = "blocked"
		x.Verification = "manual decision and evidence required"
		return x, l.finishRepair(ctx, x)
	}
	// A saved plan is only executable against the same source evidence. A fresh
	// comparison also catches a provider amount mismatch before any lookup.
	run, err := l.RunReconciliation(ctx, l.now().UTC())
	if err != nil {
		return RepairOperation{}, err
	}
	current := false
	for _, f := range run.Findings {
		if f.ID == d.ID {
			current = true
			if f.Expected != x.Expected || f.Actual != x.Actual || repairAction(f) != x.Action || f.SourceRevision != x.PreconditionRevision {
				x.Status = "blocked"
				x.Verification = "source evidence changed; create a new repair request"
				return x, l.finishRepair(ctx, x)
			}
		}
	}
	paymentOperationID := ""
	if x.Action == "lookup_original_operation" || x.Action == "retry_original_capture" {
		paymentOperationID = d.ObjectID
		if d.Kind == "pending_outbox" {
			if err := l.db.QueryRowContext(ctx, `SELECT object_id FROM outbox WHERE id=? AND kind='capture'`, d.ObjectID).Scan(&paymentOperationID); err != nil {
				return RepairOperation{}, err
			}
		}
		for _, f := range run.Findings {
			if f.Kind == "provider_amount_mismatch" && f.ObjectID == paymentOperationID {
				x.Status = "blocked"
				x.Verification = "provider amount mismatch requires manual investigation"
				return x, l.finishRepair(ctx, x)
			}
		}
	}
	// Provider evidence can remove the original finding before its outcome has
	// been applied to the local payment. Check amount mismatches first, then
	// look up the original key even when that finding has disappeared.
	if !current && x.Action != "lookup_original_operation" {
		x.Status = "verified"
		x.Verification = "already absent in reconciliation " + run.ID
		return x, l.finishRepair(ctx, x)
	}
	if x.PreconditionRevision != 0 {
		var current int64
		if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, d.ObjectID).Scan(&current); err != nil {
			return RepairOperation{}, err
		}
		if current != x.PreconditionRevision {
			x.Status = "blocked"
			x.Verification = "source revision changed; rerun reconciliation"
			return x, l.finishRepair(ctx, x)
		}
	}
	switch x.Action {
	case "rebuild_entitlement":
		subID := d.ObjectID
		if d.Kind == "pending_outbox" {
			if err := l.db.QueryRowContext(ctx, `SELECT object_id FROM outbox WHERE id=? AND kind='entitlement'`, d.ObjectID).Scan(&subID); err != nil {
				return RepairOperation{}, err
			}
		}
		if err := l.refreshOneEntitlement(ctx, subID); err != nil {
			return RepairOperation{}, err
		}
	case "retry_original_capture":
		if _, err := l.DispatchCapture(ctx, paymentOperationID, ""); err != nil && !errors.Is(err, ErrPaymentUnknown) {
			return RepairOperation{}, err
		}
	case "lookup_original_operation":
		found, err := l.ReconcilePayment(ctx, paymentOperationID)
		if err != nil {
			return RepairOperation{}, err
		}
		if !found {
			x.Status = "waiting"
			x.Verification = "provider has no terminal observation for original key"
		}
	}
	if x.Status != "waiting" {
		x.Status = "executed"
	}
	run, err = l.RunReconciliation(ctx, l.now().UTC())
	if err != nil {
		return RepairOperation{}, err
	}
	stillOpen := false
	for _, f := range run.Findings {
		if f.ID == d.ID {
			stillOpen = true
			break
		}
	}
	if !stillOpen {
		x.Status = "verified"
		x.Verification = "absent in reconciliation " + run.ID
	} else if x.Status != "waiting" {
		x.Verification = "still present in reconciliation " + run.ID
	}
	return x, l.finishRepair(ctx, x)
}

func (l *Lab) finishRepair(ctx context.Context, x RepairOperation) error {
	result, err := l.db.ExecContext(ctx, `UPDATE repair_operations SET status=?,verification=?,executed_at=? WHERE id=? AND status IN ('planned','waiting')`, x.Status, x.Verification, l.now().UTC().UnixNano(), x.ID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}
