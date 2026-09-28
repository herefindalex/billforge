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

type externalDispatchSnapshot struct {
	Sources []byte
	Impact  []byte
}

func (l *Lab) loadExternalDispatchSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, actionID, targetID string) (externalDispatchSnapshot, error) {
	var snapshot externalDispatchSnapshot
	switch actionID {
	case "C09":
		var invoiceID, key, currency, status, outboxStatus string
		var amount int64
		err := tx.QueryRowContext(ctx, `SELECT o.invoice_id,o.provider_key,o.amount_minor,o.currency,o.status,b.status FROM payment_operations o JOIN outbox b ON b.object_id=o.id AND b.kind='capture' WHERE o.id=?`, targetID).Scan(&invoiceID, &key, &amount, &currency, &status, &outboxStatus)
		if err != nil {
			return snapshot, err
		}
		if status != "created" || outboxStatus != "pending" || amount <= 0 {
			return snapshot, ErrConflict
		}
		periodIndex, periodEnd, dueAt, err := loadPayablePeriod(ctx, tx, invoiceID)
		if err != nil {
			return snapshot, err
		}
		contractInvoice, err := isContractInvoice(ctx, tx, invoiceID)
		if err != nil {
			return snapshot, err
		}
		if contractInvoice && at.UnixNano() < dueAt {
			return snapshot, ErrConflict
		}
		if !contractInvoice && at.UnixNano() >= periodEnd {
			return snapshot, ErrPeriodEnded
		}
		if !contractInvoice && periodIndex > 0 && at.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
			return snapshot, ErrLateNeedsReview
		}
		snapshot.Sources, err = json.Marshal(map[string]string{
			"operation_id": targetID, "invoice_id": invoiceID, "provider_key": key, "status": status,
			"outbox_status": outboxStatus, "amount_minor": strconv.FormatInt(amount, 10), "currency": currency,
			"period_index": strconv.Itoa(periodIndex), "period_end": strconv.FormatInt(periodEnd, 10), "due_at": strconv.FormatInt(dueAt, 10),
		})
		if err != nil {
			return externalDispatchSnapshot{}, err
		}
		snapshot.Impact, err = json.Marshal(map[string]string{"operation_id": targetID, "invoice_id": invoiceID, "amount_minor": strconv.FormatInt(amount, 10), "currency": currency})
		return snapshot, err
	case "C16":
		var grantID, key, sourceKey, currency, status, outboxStatus string
		var amount int64
		err := tx.QueryRowContext(ctx, `SELECT r.grant_id,r.provider_key,r.source_provider_key,r.amount_minor,r.currency,r.status,b.status FROM refund_operations r JOIN outbox b ON b.object_id=r.id AND b.kind='refund' WHERE r.id=?`, targetID).Scan(&grantID, &key, &sourceKey, &amount, &currency, &status, &outboxStatus)
		if err != nil {
			return snapshot, err
		}
		if status != "created" || outboxStatus != "pending" || amount <= 0 {
			return snapshot, ErrConflict
		}
		snapshot.Sources, err = json.Marshal(map[string]string{
			"refund_id": targetID, "grant_id": grantID, "provider_key": key, "source_provider_key": sourceKey,
			"status": status, "outbox_status": outboxStatus, "amount_minor": strconv.FormatInt(amount, 10), "currency": currency,
		})
		if err != nil {
			return externalDispatchSnapshot{}, err
		}
		snapshot.Impact, err = json.Marshal(map[string]string{"refund_id": targetID, "grant_id": grantID, "amount_minor": strconv.FormatInt(amount, 10), "currency": currency})
		return snapshot, err
	default:
		return snapshot, ErrAdminUnsupportedAction
	}
}

func (l *Lab) adminCreateExternalDispatchPreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadExternalDispatchSnapshot(ctx, tx, l.now().UTC(), actionID, targetID)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, targetID, adminIntentHash(actionID, targetID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: targetID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) adminExternalStatus(ctx context.Context, actionID, targetID string) (string, error) {
	var status string
	var err error
	if actionID == "C09" || actionID == "C10" {
		err = l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, targetID).Scan(&status)
	} else {
		err = l.db.QueryRowContext(ctx, `SELECT status FROM refund_operations WHERE id=?`, targetID).Scan(&status)
	}
	return status, err
}

type AdminExternalOperationState struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	OutboxStatus string `json:"outbox_status"`
	AmountMinor  string `json:"amount_minor"`
	Currency     string `json:"currency"`
}

func (l *Lab) AdminExternalOperationState(ctx context.Context, actionID, targetID string) (AdminExternalOperationState, error) {
	var state AdminExternalOperationState
	var amount int64
	var query string
	switch actionID {
	case "C09":
		query = `SELECT o.id,o.status,b.status,o.amount_minor,o.currency FROM payment_operations o JOIN outbox b ON b.object_id=o.id AND b.kind='capture' WHERE o.id=?`
	case "C16":
		query = `SELECT r.id,r.status,b.status,r.amount_minor,r.currency FROM refund_operations r JOIN outbox b ON b.object_id=r.id AND b.kind='refund' WHERE r.id=?`
	default:
		return state, ErrAdminUnsupportedAction
	}
	if err := l.db.QueryRowContext(ctx, query, targetID).Scan(&state.ID, &state.Status, &state.OutboxStatus, &amount, &state.Currency); err != nil {
		return AdminExternalOperationState{}, err
	}
	state.AmountMinor = strconv.FormatInt(amount, 10)
	return state, nil
}

func (l *Lab) adminExternalDispatchOwner(ctx context.Context, actionID, targetID string) (string, error) {
	var owner string
	err := l.db.QueryRowContext(ctx, `SELECT command_id FROM admin_external_dispatch_claims WHERE action_id=? AND target_id=?`, actionID, targetID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return owner, err
}

func (l *Lab) adminClaimExternalDispatch(ctx context.Context, commandID, actionID, targetID string) (bool, error) {
	_, err := l.db.ExecContext(ctx, `INSERT INTO admin_external_dispatch_claims(action_id,target_id,command_id,created_at) VALUES(?,?,?,?) ON CONFLICT(action_id,target_id) DO NOTHING`, actionID, targetID, commandID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	owner, err := l.adminExternalDispatchOwner(ctx, actionID, targetID)
	return owner == commandID, err
}

func adminReleaseUnstartedExternalClaimTx(ctx context.Context, tx *sql.Tx, commandID, actionID, targetID string) error {
	var operationTable string
	switch actionID {
	case "C09":
		operationTable = "payment_operations"
	case "C16":
		operationTable = "refund_operations"
	default:
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM admin_external_dispatch_claims WHERE action_id=? AND target_id=? AND command_id=? AND EXISTS (SELECT 1 FROM `+operationTable+` WHERE id=? AND status='created')`, actionID, targetID, commandID, targetID)
	return err
}

func (l *Lab) adminExternalPreviewCurrent(ctx context.Context, commandID, previewID, actionID, targetID, businessTime string) (bool, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var saved string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&saved); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return false, err
	}
	snapshot, err := l.loadExternalDispatchSnapshot(ctx, tx, at, actionID, targetID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) || errors.Is(err, ErrLateNeedsReview) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return saved == string(snapshot.Sources), nil
}

func (l *Lab) adminFailStaleExternalDispatch(ctx context.Context, commandID, actorID, actionID, targetID string) (AdminCommand, error) {
	kind := "payment"
	if actionID == "C16" {
		kind = "refund"
	}
	if err := l.releaseUnusedAdminFault(ctx, commandID, kind, targetID); err != nil {
		return AdminCommand{}, err
	}
	if err := l.adminFailCommand(ctx, commandID, actorID, actionID, targetID, "PREVIEW_STALE"); err != nil {
		return AdminCommand{}, err
	}
	return l.AdminCommand(ctx, commandID)
}

func (l *Lab) adminExecuteExternalCommand(ctx context.Context, commandID string) (AdminCommand, error) {
	var actionID, actorID, targetID, status, previewID, businessTime string
	if err := l.db.QueryRowContext(ctx, `SELECT action_id,actor_id,target_id,status,COALESCE(preview_id,''),business_time FROM admin_commands WHERE id=?`, commandID).Scan(&actionID, &actorID, &targetID, &status, &previewID, &businessTime); err != nil {
		return AdminCommand{}, err
	}
	if status == "succeeded" || status == "failed" {
		return l.AdminCommand(ctx, commandID)
	}
	if status != "accepted" && status != "waiting_verification" {
		return AdminCommand{}, ErrConflict
	}
	commandAt, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return AdminCommand{}, err
	}
	if actionID == "C09" || actionID == "C16" {
		operationStatus, err := l.adminExternalStatus(ctx, actionID, targetID)
		if err != nil {
			return AdminCommand{}, err
		}
		if operationStatus == "cancelled" && status == "accepted" {
			return l.adminFailStaleExternalDispatch(ctx, commandID, actorID, actionID, targetID)
		}
		if operationStatus == "created" && status == "accepted" {
			current, err := l.adminExternalPreviewCurrent(ctx, commandID, previewID, actionID, targetID, businessTime)
			if err != nil {
				return AdminCommand{}, err
			}
			if !current {
				return l.adminFailStaleExternalDispatch(ctx, commandID, actorID, actionID, targetID)
			}
		}
		if operationStatus != "created" || status != "accepted" {
			claimed, err := l.adminClaimExternalDispatch(ctx, commandID, actionID, targetID)
			if err != nil {
				return AdminCommand{}, err
			}
			if !claimed {
				if status == "accepted" {
					return l.adminFailStaleExternalDispatch(ctx, commandID, actorID, actionID, targetID)
				}
				return AdminCommand{}, ErrConflict
			}
		}
		if operationStatus != "succeeded" && operationStatus != "definitively_failed" {
			if err := l.adminRenewLease(ctx, commandID); err != nil {
				return AdminCommand{}, err
			}
			fault := ""
			if status == "accepted" && operationStatus == "created" {
				kind := "payment"
				if actionID == "C16" {
					kind = "refund"
				}
				fault, err = l.claimAdminFault(ctx, commandID, kind, targetID)
				if errors.Is(err, ErrAdminPreviewStale) {
					return l.adminFailStaleExternalDispatch(ctx, commandID, actorID, actionID, targetID)
				}
				if err != nil {
					return AdminCommand{}, err
				}
			}
			if actionID == "C09" {
				_, err = l.dispatchCaptureAt(ctx, targetID, fault, commandAt)
			} else {
				_, err = l.dispatchRefundAt(ctx, targetID, fault, commandAt)
			}
			if errors.Is(err, ErrPaymentUnknown) {
				if err := l.adminMarkWaitingVerification(ctx, commandID, actorID, actionID, targetID); err != nil {
					return AdminCommand{}, err
				}
				return l.AdminCommand(ctx, commandID)
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				if errors.Is(err, ErrConflict) || errors.Is(err, ErrPeriodEnded) || errors.Is(err, ErrLateNeedsReview) {
					latest, lookupErr := l.adminExternalStatus(ctx, actionID, targetID)
					if lookupErr != nil {
						return AdminCommand{}, lookupErr
					}
					if latest == "cancelled" && status == "accepted" {
						return l.adminFailStaleExternalDispatch(ctx, commandID, actorID, actionID, targetID)
					}
					if latest == "created" && status == "accepted" {
						kind := "payment"
						if actionID == "C16" {
							kind = "refund"
						}
						if err := l.releaseUnusedAdminFault(ctx, commandID, kind, targetID); err != nil {
							return AdminCommand{}, err
						}
						if failErr := l.adminFailCommand(ctx, commandID, actorID, actionID, targetID, "DOMAIN_REJECTED"); failErr != nil {
							return AdminCommand{}, failErr
						}
						return l.AdminCommand(ctx, commandID)
					}
					if latest == "submitted" || latest == "unknown" {
						if waitErr := l.adminMarkWaitingVerification(ctx, commandID, actorID, actionID, targetID); waitErr != nil {
							return AdminCommand{}, waitErr
						}
						return l.AdminCommand(ctx, commandID)
					}
				} else {
					return AdminCommand{}, err
				}
			}
		}
		operationStatus, err = l.adminExternalStatus(ctx, actionID, targetID)
		if err != nil {
			return AdminCommand{}, err
		}
		if operationStatus == "cancelled" && status == "accepted" {
			return l.adminFailStaleExternalDispatch(ctx, commandID, actorID, actionID, targetID)
		}
		if operationStatus != "succeeded" && operationStatus != "definitively_failed" {
			if err := l.adminMarkWaitingVerification(ctx, commandID, actorID, actionID, targetID); err != nil {
				return AdminCommand{}, err
			}
			return l.AdminCommand(ctx, commandID)
		}
		return l.adminFinishExternalCommand(ctx, commandID, map[string]string{"target_id": targetID, "operation_status": operationStatus})
	}
	if actionID == "C10" || actionID == "C17" {
		var found bool
		var err error
		if actionID == "C10" {
			found, err = l.ReconcilePayment(ctx, targetID)
		} else {
			found, err = l.ReconcileRefund(ctx, targetID)
		}
		if errors.Is(err, sql.ErrNoRows) {
			if err := l.adminFailCommand(ctx, commandID, actorID, actionID, targetID, "DOMAIN_REJECTED"); err != nil {
				return AdminCommand{}, err
			}
			return l.AdminCommand(ctx, commandID)
		}
		if err != nil {
			return AdminCommand{}, err
		}
		operationStatus, err := l.adminExternalStatus(ctx, actionID, targetID)
		if err != nil {
			return AdminCommand{}, err
		}
		if operationStatus != "succeeded" && operationStatus != "definitively_failed" {
			if err := l.adminMarkWaitingVerification(ctx, commandID, actorID, actionID, targetID); err != nil {
				return AdminCommand{}, err
			}
			return l.AdminCommand(ctx, commandID)
		}
		return l.adminFinishExternalCommand(ctx, commandID, map[string]string{"target_id": targetID, "provider_found": strconv.FormatBool(found), "operation_status": operationStatus})
	}
	return AdminCommand{}, ErrAdminUnsupportedAction
}

func (l *Lab) adminMarkWaitingVerification(ctx context.Context, commandID, actorID, actionID, targetID string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := adminReserveWriterTx(ctx, tx, commandID); err != nil {
		return err
	}
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE admin_commands SET status='waiting_verification',error_code=NULL,updated_at=? WHERE id=? AND status='accepted'`, now, commandID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?)`, commandID, adminRequestID(ctx), commandID, actorID, actionID, targetID, "waiting_verification", now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (l *Lab) adminFinishExternalCommand(ctx context.Context, commandID string, refs any) (AdminCommand, error) {
	encoded, err := json.Marshal(refs)
	if err != nil {
		return AdminCommand{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminCommand{}, err
	}
	defer tx.Rollback()
	if err := adminReserveWriterTx(ctx, tx, commandID); err != nil {
		return AdminCommand{}, err
	}
	var actorID, actionID, targetID, status string
	if err := tx.QueryRowContext(ctx, `SELECT actor_id,action_id,target_id,status FROM admin_commands WHERE id=?`, commandID).Scan(&actorID, &actionID, &targetID, &status); err != nil {
		return AdminCommand{}, err
	}
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return AdminCommand{}, err
	}
	if status == "succeeded" {
		if err := tx.Commit(); err != nil {
			return AdminCommand{}, err
		}
		return l.AdminCommand(ctx, commandID)
	}
	if status != "accepted" && status != "waiting_verification" {
		return AdminCommand{}, ErrConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_command_receipts(command_id,domain_request_key,result_refs_json,committed_at) VALUES(?,?,?,?)`, commandID, "admin:"+commandID, string(encoded), now); err != nil {
		return AdminCommand{}, fmt.Errorf("external command receipt: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET status='succeeded',result_refs_json=?,error_code=NULL,updated_at=? WHERE id=? AND status IN ('accepted','waiting_verification')`, string(encoded), now, commandID); err != nil {
		return AdminCommand{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,after_json,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?,?)`, commandID, adminRequestID(ctx), commandID, actorID, actionID, targetID, "succeeded", string(encoded), now); err != nil {
		return AdminCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminCommand{}, err
	}
	return l.AdminCommand(ctx, commandID)
}
