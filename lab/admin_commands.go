package lab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var (
	ErrAdminIdempotencyConflict = errors.New("admin idempotency key already used for a different intent")
	ErrAdminUnsupportedAction   = errors.New("admin action is not implemented")
	ErrAdminPreviewStale        = errors.New("admin preview is stale or does not match the command")
	ErrAdminInvalidCommand      = errors.New("admin command payload is invalid")
)

type AdminCommand struct {
	ID             string          `json:"id"`
	ActorID        string          `json:"actor_id"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	RequestID      string          `json:"request_id,omitempty"`
	ActionID       string          `json:"action_id"`
	TargetID       string          `json:"target_id"`
	Status         string          `json:"status"`
	ResultRefs     json.RawMessage `json:"result_refs,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

type AdminCreateQuotePayload struct {
	CustomerID           string `json:"customer_id"`
	PlanID               string `json:"plan_id"`
	ContractVersionID    string `json:"contract_version_id,omitempty"`
	Cohort               string `json:"cohort"`
	Seats                string `json:"seats"`
	ChangeSubscriptionID string `json:"change_subscription_id,omitempty"`
	Mode                 string `json:"mode,omitempty"`
	Revision             string `json:"revision,omitempty"`
}

type AdminChangePlanPayload struct {
	QuoteID     string `json:"quote_id"`
	Fingerprint string `json:"fingerprint"`
	Revision    string `json:"revision"`
}

type AdminAcceptQuotePayload struct {
	Fingerprint string `json:"fingerprint"`
}

type AdminRecordUsagePayload struct {
	Source         string `json:"source"`
	EventID        string `json:"event_id"`
	SubscriptionID string `json:"subscription_id"`
	MeterID        string `json:"meter_id"`
	OccurredAt     string `json:"occurred_at"`
	Quantity       string `json:"quantity"`
}

type AdminRevisionPayload struct {
	Revision string `json:"revision"`
}

type AdminAmountPayload struct {
	AmountMinor string `json:"amount_minor"`
}

type AdminReductionPayload struct {
	ReductionMinor string `json:"reduction_minor"`
	Reason         string `json:"reason"`
}

type AdminApplyCreditPayload struct {
	InvoiceID   string `json:"invoice_id"`
	AmountMinor string `json:"amount_minor"`
}

type AdminReconciliationPayload struct {
	AsOf string `json:"as_of"`
}

func decodeAdminPayload(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("extra JSON value")
	}
	return nil
}

func canonicalAdminPayload(actionID string, raw json.RawMessage) ([]byte, error) {
	switch actionID {
	case "C01":
		var payload AdminCreateQuotePayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		if payload.CustomerID == "" || (payload.PlanID == "") == (payload.ContractVersionID == "") {
			return nil, errors.New("customer_id and exactly one of plan_id or contract_version_id are required")
		}
		if payload.ContractVersionID != "" && payload.Cohort != "" {
			return nil, errors.New("contract quote cannot specify cohort")
		}
		if payload.ContractVersionID == "" && payload.Cohort == "" {
			payload.Cohort = "default"
		}
		if payload.Seats == "" {
			payload.Seats = "0"
		}
		seats, err := strconv.ParseInt(payload.Seats, 10, 64)
		if err != nil || seats < 0 || (payload.ContractVersionID != "" && seats == 0) {
			return nil, errors.New("seats must be a nonnegative int64 string, positive for contracts")
		}
		payload.Seats = strconv.FormatInt(seats, 10)
		if payload.ContractVersionID != "" && (payload.ChangeSubscriptionID != "" || payload.Mode != "" || payload.Revision != "") {
			return nil, errors.New("contract quote cannot bind a subscription change")
		}
		if payload.ChangeSubscriptionID != "" || payload.Mode != "" || payload.Revision != "" {
			if payload.ChangeSubscriptionID == "" || (payload.Mode != "next_period" && payload.Mode != "immediate") {
				return nil, errors.New("change_subscription_id and valid mode are required")
			}
			revision, err := strconv.ParseInt(payload.Revision, 10, 64)
			if err != nil || revision <= 0 {
				return nil, errors.New("revision must be a positive int64 string")
			}
			payload.Revision = strconv.FormatInt(revision, 10)
		}
		return json.Marshal(payload)
	case "C02":
		var payload AdminAcceptQuotePayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		if payload.Fingerprint == "" {
			return nil, errors.New("fingerprint is required")
		}
		return json.Marshal(payload)
	case "C03", "C04":
		var payload AdminChangePlanPayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		if payload.QuoteID == "" || payload.Fingerprint == "" {
			return nil, errors.New("quote_id and fingerprint are required")
		}
		revision, err := strconv.ParseInt(payload.Revision, 10, 64)
		if err != nil || revision <= 0 {
			return nil, errors.New("revision must be a positive int64 string")
		}
		payload.Revision = strconv.FormatInt(revision, 10)
		return json.Marshal(payload)
	case "C05", "C06":
		var payload AdminRevisionPayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		revision, err := strconv.ParseInt(payload.Revision, 10, 64)
		if err != nil || revision <= 0 {
			return nil, errors.New("revision must be a positive int64 string")
		}
		payload.Revision = strconv.FormatInt(revision, 10)
		return json.Marshal(payload)
	case "C07", "C15":
		var payload AdminAmountPayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		amount, err := strconv.ParseInt(payload.AmountMinor, 10, 64)
		if err != nil || amount <= 0 {
			return nil, errors.New("amount_minor must be a positive int64 string")
		}
		payload.AmountMinor = strconv.FormatInt(amount, 10)
		return json.Marshal(payload)
	case "C08", "C09", "C10", "C13", "C14", "C16", "C17", "C30", "C32", "C44", "C45":
		var payload struct{}
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		return []byte(`{}`), nil
	case "C18", "C19", "C20", "C21":
		return canonicalAdminCatalogPayload(actionID, raw)
	case "C22", "C24", "C25":
		return canonicalAdminMigrationPayload(actionID, raw)
	case "C27", "C28", "C29":
		return canonicalAdminUsagePayload(actionID, raw)
	case "C31":
		return canonicalAdminContractPayload(raw)
	case "C33":
		var payload AdminReconciliationPayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		canonical, err := canonicalAdminUTC(payload.AsOf)
		if err != nil {
			return nil, ErrAdminInvalidCommand
		}
		payload.AsOf = canonical
		return json.Marshal(payload)
	case "C34":
		return canonicalAdminRepairPayload(raw)
	case "C35":
		return canonicalAdminManualDecisionPayload(raw)
	case "C36":
		return canonicalAdminLinkAccountPayload(raw)
	case "C37", "C38":
		return canonicalAdminShadowPayload(actionID, raw)
	case "C39", "C40":
		return canonicalAdminProvenancePayload(actionID, raw)
	case "C41", "C42", "C43":
		return canonicalAdminCutoverPayload(actionID, raw)
	case "C46", "C47", "C48", "C49":
		return canonicalAdminControlPayload(actionID, raw)
	case "C11":
		var payload AdminReductionPayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		amount, err := strconv.ParseInt(payload.ReductionMinor, 10, 64)
		if err != nil || amount <= 0 || strings.TrimSpace(payload.Reason) == "" {
			return nil, errors.New("positive reduction_minor and reason are required")
		}
		payload.ReductionMinor = strconv.FormatInt(amount, 10)
		payload.Reason = strings.TrimSpace(payload.Reason)
		return json.Marshal(payload)
	case "C12":
		var payload AdminApplyCreditPayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		amount, err := strconv.ParseInt(payload.AmountMinor, 10, 64)
		if err != nil || amount <= 0 || payload.InvoiceID == "" {
			return nil, errors.New("invoice_id and positive amount_minor are required")
		}
		payload.AmountMinor = strconv.FormatInt(amount, 10)
		return json.Marshal(payload)
	case "C23":
		var payload struct{}
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		return []byte(`{}`), nil
	case "C26":
		var payload AdminRecordUsagePayload
		if err := decodeAdminPayload(raw, &payload); err != nil {
			return nil, err
		}
		if payload.Source == "" || payload.EventID == "" || payload.SubscriptionID == "" || payload.MeterID == "" {
			return nil, errors.New("usage identity fields are required")
		}
		quantity, err := strconv.ParseInt(payload.Quantity, 10, 64)
		if err != nil || quantity <= 0 {
			return nil, errors.New("quantity must be a positive int64 string")
		}
		canonical, err := canonicalAdminUTC(payload.OccurredAt)
		if err != nil {
			return nil, errors.New("occurred_at must be exact RFC3339 UTC within int64 nanoseconds")
		}
		payload.Quantity = strconv.FormatInt(quantity, 10)
		payload.OccurredAt = canonical
		return json.Marshal(payload)
	default:
		return nil, ErrAdminUnsupportedAction
	}
}

func scanAdminCommand(row interface{ Scan(...any) error }) (AdminCommand, error) {
	var c AdminCommand
	var refs string
	err := row.Scan(&c.ID, &c.ActorID, &c.IdempotencyKey, &c.RequestID, &c.ActionID, &c.TargetID, &c.Status, &refs, &c.ErrorCode, &c.CreatedAt, &c.UpdatedAt)
	if refs != "" {
		c.ResultRefs = json.RawMessage(refs)
	}
	return c, err
}

const adminCommandSelect = `SELECT id,actor_id,idempotency_key,COALESCE(request_id,''),action_id,target_id,status,COALESCE(result_refs_json,''),COALESCE(error_code,''),created_at,updated_at FROM admin_commands`

func (l *Lab) AdminCommandPayload(ctx context.Context, id string) (json.RawMessage, error) {
	var payload string
	err := l.db.QueryRowContext(ctx, `SELECT payload_json FROM admin_commands WHERE id=?`, id).Scan(&payload)
	return json.RawMessage(payload), err
}

func (l *Lab) AdminCommand(ctx context.Context, id string) (AdminCommand, error) {
	return scanAdminCommand(l.db.QueryRowContext(ctx, adminCommandSelect+` WHERE id=?`, id))
}

func (l *Lab) AdminCommands(ctx context.Context, limit int) ([]AdminCommand, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("command list limit must be 1–100")
	}
	rows, err := l.db.QueryContext(ctx, adminCommandSelect+` ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]AdminCommand, 0)
	for rows.Next() {
		command, err := scanAdminCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

// AdminCommandsPage walks commands in insertion order. The rowid cursor keeps
// already-seen pages stable when newer commands arrive between requests.
func (l *Lab) AdminCommandsPage(ctx context.Context, after int64, limit int) ([]AdminCommand, int64, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, 0, ErrAdminInvalidCommand
	}
	rows, err := l.db.QueryContext(ctx, `SELECT rowid,id,actor_id,idempotency_key,COALESCE(request_id,''),action_id,target_id,status,COALESCE(result_refs_json,''),COALESCE(error_code,''),created_at,updated_at FROM admin_commands WHERE (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT ?`, after, after, limit+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	commands := make([]AdminCommand, 0, limit)
	var lastRowID int64
	hasMore := false
	for rows.Next() {
		var rowID int64
		var command AdminCommand
		var refs string
		if err := rows.Scan(&rowID, &command.ID, &command.ActorID, &command.IdempotencyKey, &command.RequestID, &command.ActionID, &command.TargetID, &command.Status, &refs, &command.ErrorCode, &command.CreatedAt, &command.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if len(commands) == limit {
			hasMore = true
			break
		}
		if refs != "" {
			command.ResultRefs = json.RawMessage(refs)
		}
		commands = append(commands, command)
		lastRowID = rowID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if hasMore {
		return commands, lastRowID, nil
	}
	return commands, 0, nil
}

func (l *Lab) AdminSubmitCommand(ctx context.Context, actorID, key, actionID, targetID string, payload json.RawMessage, previewID string) (AdminCommand, bool, error) {
	if actorID == "" || len(key) < 8 || len(key) > 128 || strings.IndexFunc(key, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return AdminCommand{}, false, ErrAdminInvalidCommand
	}
	canonical, err := canonicalAdminPayload(actionID, payload)
	if err != nil {
		if errors.Is(err, ErrAdminUnsupportedAction) {
			return AdminCommand{}, false, err
		}
		return AdminCommand{}, false, fmt.Errorf("%w: %v", ErrAdminInvalidCommand, err)
	}
	switch actionID {
	case "C01":
		if targetID != "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C13", "C30", "C31", "C32", "C36", "C44", "C45":
		if targetID != "" || previewID == "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C18", "C19", "C20", "C21", "C22":
		if targetID != "" || previewID == "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C02", "C03", "C04", "C05", "C06", "C07", "C08", "C09", "C11", "C12", "C14", "C15", "C16", "C24", "C25", "C34", "C35":
		if targetID == "" || previewID == "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C23":
		if targetID == "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C10", "C17":
		if targetID == "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C26":
		if targetID != "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C33":
		if targetID != "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C37", "C38":
		if targetID == "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C39", "C40", "C41", "C42", "C43":
		if targetID == "" || previewID == "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C27":
		if targetID != "" || previewID == "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C46":
		if targetID != "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C47", "C48", "C49":
		if targetID == "" || previewID != "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	case "C28", "C29":
		if targetID == "" || previewID == "" {
			return AdminCommand{}, false, ErrAdminInvalidCommand
		}
	}
	hashInput, err := json.Marshal([]any{actionID, targetID, previewID, json.RawMessage(canonical)})
	if err != nil {
		return AdminCommand{}, false, err
	}
	sum := sha256.Sum256(hashInput)
	payloadHash := hex.EncodeToString(sum[:])
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminCommand{}, false, err
	}
	defer tx.Rollback()
	var existingID, existingHash string
	err = tx.QueryRowContext(ctx, `SELECT id,payload_hash FROM admin_commands WHERE actor_id=? AND idempotency_key=?`, actorID, key).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != payloadHash {
			return AdminCommand{}, false, ErrAdminIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return AdminCommand{}, false, err
		}
		c, err := l.AdminCommand(ctx, existingID)
		return c, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AdminCommand{}, false, err
	}
	businessAt, clockRevision, err := l.adminBusinessSnapshotTx(ctx, tx)
	if err != nil {
		return AdminCommand{}, false, err
	}
	if previewID != "" {
		intentHash := adminIntentHash(actionID, targetID, canonical)
		var previewActor, previewAction, previewTarget, savedHash, expiry, claimed string
		var previewRevision int64
		err := tx.QueryRowContext(ctx, `SELECT actor_id,action_id,target_id,intent_hash,expires_at,COALESCE(claimed_command_id,''),clock_revision FROM admin_previews WHERE id=?`, previewID).Scan(&previewActor, &previewAction, &previewTarget, &savedHash, &expiry, &claimed, &previewRevision)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return AdminCommand{}, false, ErrAdminPreviewStale
			}
			return AdminCommand{}, false, err
		}
		expires, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil || previewActor != actorID || previewAction != actionID || previewTarget != targetID || savedHash != intentHash || claimed != "" || previewRevision != clockRevision || !time.Now().UTC().Before(expires) {
			return AdminCommand{}, false, ErrAdminPreviewStale
		}
	}
	id, err := newID("cmd_")
	if err != nil {
		return AdminCommand{}, false, err
	}
	requestID, err := commandRequestID(ctx)
	if err != nil {
		return AdminCommand{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	businessTime := businessAt.Format(time.RFC3339Nano)
	var previewValue any
	if previewID != "" {
		previewValue = previewID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_commands(id,actor_id,idempotency_key,request_id,action_id,target_id,payload_json,payload_hash,preview_id,status,business_time,clock_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, actorID, key, requestID, actionID, targetID, string(canonical), payloadHash, previewValue, "accepted", businessTime, clockRevision, now, now)
	if err != nil {
		return AdminCommand{}, false, err
	}
	if previewID != "" {
		result, err := tx.ExecContext(ctx, `UPDATE admin_previews SET claimed_command_id=? WHERE id=? AND claimed_command_id IS NULL`, id, previewID)
		if err != nil {
			return AdminCommand{}, false, err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return AdminCommand{}, false, ErrAdminPreviewStale
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,recorded_at) VALUES(?,?,?,?,?,?,?)`, id, requestID, actorID, actionID, targetID, "accepted", now)
	if err != nil {
		return AdminCommand{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AdminCommand{}, false, err
	}
	c, err := l.AdminCommand(ctx, id)
	return c, false, err
}

func (l *Lab) AdminExecuteCommand(ctx context.Context, id string) (AdminCommand, error) {
	l.adminExecutionMu.Lock()
	defer l.adminExecutionMu.Unlock()
	generation, err := l.adminAcquireLease(ctx, id)
	if err != nil {
		return AdminCommand{}, fmt.Errorf("acquire admin command lease: %w", err)
	}
	defer l.adminReleaseLease(id, generation)
	ctx = context.WithValue(ctx, adminLeaseContextKey{}, generation)
	ctx = context.WithValue(ctx, adminLeaseCommandContextKey{}, id)
	var externalAction string
	if err := l.db.QueryRowContext(ctx, `SELECT action_id FROM admin_commands WHERE id=?`, id).Scan(&externalAction); err != nil {
		return AdminCommand{}, err
	}
	if externalAction == "C09" || externalAction == "C10" || externalAction == "C16" || externalAction == "C17" {
		return l.adminExecuteExternalCommand(ctx, id)
	}
	if externalAction == "C34" {
		return l.adminExecuteRepairCommand(ctx, id)
	}
	if externalAction == "C47" || externalAction == "C48" {
		return l.adminExecuteProviderControl(ctx, id)
	}
	if externalAction == "C13" || externalAction == "C30" || externalAction == "C32" || externalAction == "C44" || externalAction == "C45" {
		return l.adminExecuteBatchCommand(ctx, id)
	}
	var reconSnapshot ReconciliationRun
	var reconErr error
	if externalAction == "C33" {
		var raw, currentStatus string
		if err := l.db.QueryRowContext(ctx, `SELECT payload_json,status FROM admin_commands WHERE id=?`, id).Scan(&raw, &currentStatus); err != nil {
			return AdminCommand{}, err
		}
		if currentStatus == "accepted" {
			var payload AdminReconciliationPayload
			if err := decodeAdminPayload(json.RawMessage(raw), &payload); err != nil {
				return AdminCommand{}, err
			}
			at, err := time.Parse(time.RFC3339Nano, payload.AsOf)
			if err != nil {
				return AdminCommand{}, err
			}
			reconSnapshot, reconErr = l.reconciliationSnapshot(ctx, at)
		}
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminCommand{}, err
	}
	defer tx.Rollback()
	// Take the SQLite writer reservation before reading command and domain
	// sources. Another worker may acquire a lease for a different command;
	// without this, a deferred read transaction cannot upgrade its snapshot.
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET lease_generation=lease_generation WHERE id=? AND status='accepted'`, id); err != nil {
		return AdminCommand{}, err
	}
	var actionID, actorID, targetID, status, payloadJSON, businessTime, previewID string
	err = tx.QueryRowContext(ctx, `SELECT action_id,actor_id,target_id,status,payload_json,business_time,COALESCE(preview_id,'') FROM admin_commands WHERE id=?`, id).Scan(&actionID, &actorID, &targetID, &status, &payloadJSON, &businessTime, &previewID)
	if err != nil {
		return AdminCommand{}, err
	}
	if err := adminCheckLeaseTx(ctx, tx, id); err != nil {
		return AdminCommand{}, err
	}
	if status != "accepted" {
		if err := tx.Commit(); err != nil {
			return AdminCommand{}, err
		}
		return l.AdminCommand(ctx, id)
	}
	var refs any
	var domainErr error
	switch actionID {
	case "C01":
		var payload AdminCreateQuotePayload
		if err := decodeAdminPayload(json.RawMessage(payloadJSON), &payload); err != nil {
			return AdminCommand{}, err
		}
		seats, err := strconv.ParseInt(payload.Seats, 10, 64)
		if err != nil {
			return AdminCommand{}, err
		}
		at, err := time.Parse(time.RFC3339Nano, businessTime)
		if err != nil {
			return AdminCommand{}, err
		}
		var quote Quote
		if payload.ContractVersionID != "" {
			quote, domainErr = l.createContractQuoteTx(ctx, tx, at, payload.CustomerID, payload.ContractVersionID, seats)
		} else {
			quote, domainErr = l.createQuoteForCohort(ctx, tx, at, payload.CustomerID, payload.PlanID, payload.Cohort, seats)
		}
		if domainErr == nil {
			result := map[string]any{"quote_id": quote.ID, "fingerprint": quote.Fingerprint, "price_version_id": quote.PriceVersionID}
			if payload.ContractVersionID != "" {
				result["contract_version_id"] = payload.ContractVersionID
			}
			if payload.ChangeSubscriptionID != "" {
				revision, parseErr := strconv.ParseInt(payload.Revision, 10, 64)
				if parseErr != nil {
					return AdminCommand{}, parseErr
				}
				binding, bindErr := l.bindChangeQuoteTx(ctx, tx, at, quote.ID, payload.ChangeSubscriptionID, payload.Mode, revision)
				domainErr = bindErr
				if bindErr == nil {
					result["binding_fingerprint"] = binding.Fingerprint
					result["change_subscription_id"] = binding.SubscriptionID
					result["mode"] = binding.Mode
				}
			}
			refs = result
		}
	case "C02":
		refs, domainErr = l.executeAcceptQuoteTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C03":
		refs, domainErr = l.executeSchedulePlanTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C04":
		refs, domainErr = l.executeImmediateUpgradeTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C05":
		refs, domainErr = l.executeScheduleCancelTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C06":
		refs, domainErr = l.executeResumeCancelTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C07":
		refs, domainErr = l.executeCreatePaymentTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C08":
		refs, domainErr = l.executeRetryPaymentTx(ctx, tx, id, previewID, targetID, businessTime)
	case "C11":
		refs, domainErr = l.executePostReductionTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C12":
		refs, domainErr = l.executeApplyCreditTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C14":
		refs, domainErr = l.executeResolveUnfulfilledTx(ctx, tx, id, previewID, targetID, businessTime)
	case "C15":
		refs, domainErr = l.executeReserveRefundTx(ctx, tx, id, previewID, targetID, payloadJSON, businessTime)
	case "C18", "C19", "C20", "C21":
		refs, domainErr = l.executeAdminCatalogTx(ctx, tx, id, previewID, actionID, payloadJSON, businessTime)
	case "C22", "C24", "C25":
		refs, domainErr = l.executeAdminMigrationTx(ctx, tx, id, previewID, actionID, targetID, payloadJSON, businessTime)
	case "C27", "C28", "C29":
		refs, domainErr = l.executeAdminUsageTx(ctx, tx, id, previewID, actionID, targetID, payloadJSON, businessTime)
	case "C31":
		refs, domainErr = l.executeAdminContractTx(ctx, tx, id, actorID, previewID, []byte(payloadJSON))
	case "C33":
		if reconErr != nil {
			domainErr = reconErr
		} else {
			var run ReconciliationRun
			run, domainErr = l.persistReconciliationTx(ctx, tx, reconSnapshot)
			if domainErr == nil {
				refs = map[string]string{"run_id": run.ID, "finding_count": strconv.Itoa(len(run.Findings))}
			}
		}
	case "C35":
		refs, domainErr = l.executeManualDecisionTx(ctx, tx, id, actorID, targetID, previewID, []byte(payloadJSON))
	case "C36":
		refs, domainErr = l.executeAccountLinkTx(ctx, tx, id, actorID, previewID, []byte(payloadJSON))
	case "C37", "C38":
		refs, domainErr = l.executeAdminShadowTx(ctx, tx, actionID, targetID, []byte(payloadJSON))
	case "C39", "C40":
		refs, domainErr = l.executeAdminProvenanceTx(ctx, tx, id, actorID, actionID, targetID, previewID, []byte(payloadJSON))
	case "C41", "C42", "C43":
		refs, domainErr = l.executeAdminCutoverTx(ctx, tx, id, actorID, actionID, targetID, previewID, []byte(payloadJSON))
	case "C46":
		refs, domainErr = l.executeAdminClockTx(ctx, tx, payloadJSON)
	case "C49":
		refs, domainErr = l.executeAdminFaultTx(ctx, tx, id, targetID, payloadJSON)
	case "C23":
		domainErr = pausePriceMigrationTx(ctx, tx, targetID)
		if domainErr == nil {
			refs = map[string]string{"migration_id": targetID, "status": "paused"}
		}
	case "C26":
		var payload AdminRecordUsagePayload
		if err := decodeAdminPayload(json.RawMessage(payloadJSON), &payload); err != nil {
			return AdminCommand{}, err
		}
		quantity, err := strconv.ParseInt(payload.Quantity, 10, 64)
		if err != nil {
			return AdminCommand{}, err
		}
		eventAt, err := time.Parse(time.RFC3339Nano, payload.OccurredAt)
		if err != nil {
			return AdminCommand{}, err
		}
		businessAt, err := time.Parse(time.RFC3339Nano, businessTime)
		if err != nil {
			return AdminCommand{}, err
		}
		var event UsageEvent
		event, domainErr = l.recordUsageTx(ctx, tx, businessAt, payload.Source, payload.EventID, payload.SubscriptionID, payload.MeterID, quantity, eventAt)
		if domainErr == nil {
			refs = map[string]string{"source": event.Source, "event_id": event.EventID, "subscription_id": event.SubscriptionID, "meter_id": event.MeterID, "quantity": strconv.FormatInt(event.Quantity, 10), "period_index": strconv.Itoa(event.PeriodIndex), "price_version_id": event.PriceVersionID}
		}
	default:
		return AdminCommand{}, ErrAdminUnsupportedAction
	}
	if domainErr != nil {
		_ = tx.Rollback()
		if errors.Is(domainErr, sql.ErrNoRows) || errors.Is(domainErr, ErrConflict) || errors.Is(domainErr, ErrExpired) || errors.Is(domainErr, ErrPeriodEnded) || errors.Is(domainErr, ErrAdminPreviewStale) || errors.Is(domainErr, ErrAccountMigrationStopped) {
			code := "DOMAIN_REJECTED"
			if errors.Is(domainErr, ErrAccountMigrationStopped) {
				code = "ACCOUNT_MIGRATION_STOPPED"
			} else if actionID == "C02" || actionID == "C03" || actionID == "C04" || actionID == "C05" || actionID == "C06" || actionID == "C07" || actionID == "C08" || actionID == "C11" || actionID == "C12" || actionID == "C13" || actionID == "C14" || actionID == "C15" || actionID == "C18" || actionID == "C19" || actionID == "C20" || actionID == "C21" || actionID == "C22" || actionID == "C24" || actionID == "C25" || actionID == "C27" || actionID == "C28" || actionID == "C29" || actionID == "C30" || actionID == "C31" || actionID == "C32" || actionID == "C35" || actionID == "C36" || actionID == "C39" || actionID == "C40" || actionID == "C41" || actionID == "C42" || actionID == "C43" || actionID == "C44" || actionID == "C45" {
				code = "PREVIEW_STALE"
			}
			if err := l.adminFailCommand(ctx, id, actorID, actionID, targetID, code); err != nil {
				return AdminCommand{}, err
			}
			return l.AdminCommand(ctx, id)
		}
		return AdminCommand{}, domainErr
	}
	resultJSON, err := json.Marshal(refs)
	if err != nil {
		return AdminCommand{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_command_receipts(command_id,domain_request_key,result_refs_json,committed_at) VALUES(?,?,?,?)`, id, "admin:"+id, string(resultJSON), now)
	if err != nil {
		return AdminCommand{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE admin_commands SET status='succeeded',result_refs_json=?,error_code=NULL,updated_at=? WHERE id=? AND status='accepted'`, string(resultJSON), now, id)
	if err != nil {
		return AdminCommand{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,after_json,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?,?)`, id, adminRequestID(ctx), id, actorID, actionID, targetID, "succeeded", string(resultJSON), now)
	if err != nil {
		return AdminCommand{}, err
	}
	if actionID == "C46" {
		var control adminClockPayload
		if err := decodeAdminPayload(json.RawMessage(payloadJSON), &control); err != nil {
			return AdminCommand{}, err
		}
		revision := refs.(map[string]any)["revision"].(int64)
		l.clockMu.Lock()
		if err := tx.Commit(); err != nil {
			l.clockMu.Unlock()
			return AdminCommand{}, err
		}
		if control.Mode == "fixed" {
			at, _ := time.Parse(time.RFC3339Nano, control.ValueUTC)
			l.fixedClock = &at
		} else {
			l.fixedClock = nil
		}
		l.clockRevision = revision
		l.clockMu.Unlock()
	} else if err := tx.Commit(); err != nil {
		return AdminCommand{}, err
	}
	return l.AdminCommand(ctx, id)
}

func (l *Lab) executeScheduleCancelTx(ctx context.Context, tx *sql.Tx, commandID, previewID, subID, payloadJSON, businessTime string) (any, error) {
	var input AdminRevisionPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
	var sourceJSON string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&sourceJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	var sources map[string]string
	if err := json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
		return nil, err
	}
	var revision int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&revision, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	if sources["subscription_revision"] != strconv.FormatInt(revision, 10) || sources["subscription_status"] != status || input.Revision != sources["subscription_revision"] {
		return nil, ErrAdminPreviewStale
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	effective, err := currentPeriodEnd(ctx, tx, subID, at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if sources["period_end"] != strconv.FormatInt(effective, 10) {
		return nil, ErrAdminPreviewStale
	}
	result, err := l.scheduleCancelTx(ctx, tx, at, subID, revision, "admin:"+commandID)
	if err != nil {
		return nil, err
	}
	return map[string]string{"schedule_id": result.ID, "subscription_id": subID, "effective_at": result.EffectiveAt.UTC().Format(time.RFC3339Nano), "revision": strconv.FormatInt(result.Revision, 10)}, nil
}

func (l *Lab) executeResumeCancelTx(ctx context.Context, tx *sql.Tx, commandID, previewID, subID, payloadJSON, businessTime string) (any, error) {
	var input AdminRevisionPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
	var sourceJSON string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&sourceJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	var sources map[string]string
	if err := json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
		return nil, err
	}
	var revision int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&revision, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	if sources["subscription_revision"] != strconv.FormatInt(revision, 10) || sources["subscription_status"] != status || input.Revision != sources["subscription_revision"] {
		return nil, ErrAdminPreviewStale
	}
	var scheduleID string
	var effective int64
	if err := tx.QueryRowContext(ctx, `SELECT id,effective_at FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'`, subID).Scan(&scheduleID, &effective); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	if sources["schedule_id"] != scheduleID || sources["effective_at"] != strconv.FormatInt(effective, 10) {
		return nil, ErrAdminPreviewStale
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	if at.UnixNano() >= effective {
		return nil, ErrAdminPreviewStale
	}
	result, err := l.resumeCancelTx(ctx, tx, at, subID, revision, "admin:"+commandID)
	if err != nil {
		return nil, err
	}
	return map[string]string{"schedule_id": result.ID, "subscription_id": subID, "status": "cancelled", "revision": strconv.FormatInt(result.Revision, 10)}, nil
}

func (l *Lab) executeAcceptQuoteTx(ctx context.Context, tx *sql.Tx, commandID, previewID, quoteID, payloadJSON, businessTime string) (any, error) {
	var payload AdminAcceptQuotePayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &payload); err != nil {
		return nil, err
	}
	var sourceJSON, impactJSON string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json,impact_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&sourceJSON, &impactJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	var sources, impact map[string]string
	if err := json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(impactJSON), &impact); err != nil {
		return nil, err
	}
	var fingerprint, priceID, checksum string
	var contractID, contractChecksum sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT q.fingerprint,q.price_version_id,p.checksum,cv.id,cv.checksum FROM quotes q JOIN price_versions p ON p.id=q.price_version_id LEFT JOIN contract_quotes cq ON cq.quote_id=q.id LEFT JOIN contract_versions cv ON cv.id=cq.contract_version_id WHERE q.id=?`, quoteID).Scan(&fingerprint, &priceID, &checksum, &contractID, &contractChecksum)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if sources["quote_fingerprint"] != fingerprint || sources["price_version_id"] != priceID || sources["price_checksum"] != checksum || sources["contract_version_id"] != contractID.String || sources["contract_checksum"] != contractChecksum.String || payload.Fingerprint != fingerprint || impact["quote_id"] != quoteID || impact["contract_version_id"] != contractID.String {
		return nil, ErrAdminPreviewStale
	}
	var boundChange int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM change_quote_bindings WHERE quote_id=?)`, quoteID).Scan(&boundChange); err != nil {
		return nil, err
	}
	if boundChange != 0 {
		return nil, ErrAdminPreviewStale
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	var receipt Receipt
	if contractID.Valid {
		if impact["payment_terms"] != "net30" {
			return nil, ErrAdminPreviewStale
		}
		receipt, err = l.acceptContractQuoteTx(ctx, tx, at, quoteID, payload.Fingerprint, "admin:"+commandID)
	} else {
		receipt, err = l.acceptQuoteTx(ctx, tx, at, quoteID, payload.Fingerprint, "admin:"+commandID)
	}
	if err != nil {
		return nil, err
	}
	if impact["amount_minor"] != strconv.FormatInt(receipt.AmountMinor, 10) || impact["currency"] != receipt.Currency {
		return nil, ErrAdminPreviewStale
	}
	refs := map[string]string{
		"subscription_id":     receipt.SubscriptionID,
		"invoice_id":          receipt.InvoiceID,
		"operation_id":        receipt.OperationID,
		"amount_minor":        strconv.FormatInt(receipt.AmountMinor, 10),
		"currency":            receipt.Currency,
		"contract_version_id": contractID.String,
	}
	if contractID.Valid {
		var dueNano int64
		if err := tx.QueryRowContext(ctx, `SELECT due_at FROM billing_periods WHERE subscription_id=? AND period_index=0`, receipt.SubscriptionID).Scan(&dueNano); err != nil {
			return nil, err
		}
		refs["due_now_minor"] = "0"
		refs["first_due_at"] = time.Unix(0, dueNano).UTC().Format(time.RFC3339Nano)
	}
	return refs, nil
}

func (l *Lab) adminFailCommand(ctx context.Context, id, actorID, actionID, targetID, code string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := adminReserveWriterTx(ctx, tx, id); err != nil {
		return err
	}
	if err := adminCheckLeaseTx(ctx, tx, id); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET status='failed',error_code=?,updated_at=? WHERE id=? AND status='accepted'`, code, now, id); err != nil {
		return err
	}
	if err := adminReleaseUnstartedExternalClaimTx(ctx, tx, id, actionID, targetID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?)`, id, adminRequestID(ctx), id, actorID, actionID, targetID, code, now); err != nil {
		return err
	}
	return tx.Commit()
}

// AdminRevokeUnstarted revokes only commands whose effect has not begun. A
// submitted provider operation must remain available for verification even
// when the actor's current capability policy no longer permits new work.
func (l *Lab) AdminRevokeUnstarted(ctx context.Context, id string) (bool, error) {
	l.adminExecutionMu.Lock()
	defer l.adminExecutionMu.Unlock()
	generation, err := l.adminAcquireLease(ctx, id)
	if err != nil {
		return false, err
	}
	defer l.adminReleaseLease(id, generation)
	if generation == 0 {
		return false, nil
	}
	ctx = context.WithValue(ctx, adminLeaseContextKey{}, generation)
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var actorID, actionID, targetID, status string
	if err := tx.QueryRowContext(ctx, `SELECT actor_id,action_id,target_id,status FROM admin_commands WHERE id=?`, id).Scan(&actorID, &actionID, &targetID, &status); err != nil {
		return false, err
	}
	if err := adminCheckLeaseTx(ctx, tx, id); err != nil {
		return false, err
	}
	if status != "accepted" {
		return false, tx.Commit()
	}
	// Reconciliation and provider control can already have an external
	// obligation or a receipt in a separate database. Keep resolving them.
	switch actionID {
	case "C10", "C17", "C34", "C47", "C48":
		return false, tx.Commit()
	case "C09", "C16":
		var operationStatus string
		table := "payment_operations"
		if actionID == "C16" {
			table = "refund_operations"
		}
		if err := tx.QueryRowContext(ctx, `SELECT status FROM `+table+` WHERE id=?`, targetID).Scan(&operationStatus); err != nil {
			return false, err
		}
		if operationStatus != "created" {
			return false, tx.Commit()
		}
		kind := "payment"
		if actionID == "C16" {
			kind = "refund"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=NULL WHERE operation_kind=? AND operation_id=? AND claimed_command_id=?`, kind, targetID, id); err != nil {
			return false, err
		}
		if err := adminReleaseUnstartedExternalClaimTx(ctx, tx, id, actionID, targetID); err != nil {
			return false, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE admin_commands SET status='failed',error_code='PERMISSION_REVOKED',updated_at=? WHERE id=? AND status='accepted'`, now, id)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return false, ErrAdminLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?)`, id, adminRequestID(ctx), id, actorID, actionID, targetID, "PERMISSION_REVOKED", now); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (l *Lab) AdminResumeAccepted(ctx context.Context) error {
	return l.AdminResumeWithPolicy(ctx, nil)
}

func (l *Lab) adminMayResumeRevoked(ctx context.Context, command AdminCommand) (bool, error) {
	if command.Status == "waiting_verification" {
		return l.adminMayVerifyWaiting(ctx, command)
	}
	if command.Status != "accepted" {
		return true, nil
	}
	switch command.ActionID {
	case "C34":
		var status string
		err := l.db.QueryRowContext(ctx, `SELECT status FROM repair_operations WHERE request_key=?`, "admin:"+command.ID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		// A saved plan may still dispatch a provider request. Read-only
		// verification may inspect only a waiting or completed repair.
		return status != "planned", nil
	case "C47", "C48":
		var found int
		err := l.provider.db.QueryRowContext(ctx, `SELECT 1 FROM provider_control_receipts WHERE command_id=?`, command.ID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			// A worker may still be crossing the separate provider DB
			// boundary. Hold the command instead of initiating a decision.
			return false, nil
		}
		return err == nil, err
	default:
		return true, nil
	}
}

func (l *Lab) adminMayVerifyWaiting(ctx context.Context, command AdminCommand) (bool, error) {
	switch command.ActionID {
	case "C09", "C16":
		status, err := l.adminExternalStatus(ctx, command.ActionID, command.TargetID)
		return status != "created", err
	case "C10", "C17":
		return true, nil // Original-operation lookup only.
	case "C34":
		var status string
		err := l.db.QueryRowContext(ctx, `SELECT status FROM repair_operations WHERE request_key=?`, "admin:"+command.ID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return status != "planned", err
	default:
		return false, nil
	}
}

func (l *Lab) adminMarkRevokedHold(ctx context.Context, id string) error {
	l.adminExecutionMu.Lock()
	defer l.adminExecutionMu.Unlock()
	generation, err := l.adminAcquireLease(ctx, id)
	if err != nil {
		return err
	}
	defer l.adminReleaseLease(id, generation)
	if generation == 0 {
		return nil
	}
	ctx = context.WithValue(ctx, adminLeaseContextKey{}, generation)
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actorID, actionID, targetID, status, code string
	if err := tx.QueryRowContext(ctx, `SELECT actor_id,action_id,target_id,status,COALESCE(error_code,'') FROM admin_commands WHERE id=?`, id).Scan(&actorID, &actionID, &targetID, &status, &code); err != nil {
		return err
	}
	if err := adminCheckLeaseTx(ctx, tx, id); err != nil {
		return err
	}
	if status != "accepted" || code == "PERMISSION_REVOKED_REVIEW" {
		return tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE admin_commands SET error_code='PERMISSION_REVOKED_REVIEW',updated_at=? WHERE id=? AND status='accepted'`, now, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrAdminLeaseLost
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,request_id,actor_id,action_id,target_id,reason,recorded_at) VALUES(?,COALESCE(NULLIF(?,''),(SELECT request_id FROM admin_commands WHERE id=?)),?,?,?,?,?)`, id, adminRequestID(ctx), id, actorID, actionID, targetID, "PERMISSION_REVOKED_REVIEW", now); err != nil {
		return err
	}
	return tx.Commit()
}

// AdminVerifyExistingObligation only resumes commands whose durable state
// proves that verification cannot initiate a new provider operation.
func (l *Lab) AdminVerifyExistingObligation(ctx context.Context, id string) (AdminCommand, error) {
	command, err := l.AdminCommand(ctx, id)
	if err != nil {
		return AdminCommand{}, err
	}
	if command.Status == "accepted" && command.ErrorCode == "PERMISSION_REVOKED_REVIEW" {
		switch command.ActionID {
		case "C34", "C47", "C48":
		default:
			return AdminCommand{}, ErrConflict
		}
		mayResume, err := l.adminMayResumeRevoked(ctx, command)
		if err != nil || !mayResume {
			return command, err
		}
		return l.AdminExecuteCommand(ctx, id)
	}
	if command.Status != "waiting_verification" {
		return AdminCommand{}, ErrConflict
	}
	mayResume, err := l.adminMayVerifyWaiting(ctx, command)
	if err != nil {
		return AdminCommand{}, err
	}
	if !mayResume {
		return AdminCommand{}, ErrConflict
	}
	return l.AdminExecuteCommand(ctx, id)
}

// AdminResumeWithPolicy rechecks the current capability policy after restart.
// A nil policy preserves the original unrestricted local recovery behavior.
func (l *Lab) AdminResumeWithPolicy(ctx context.Context, allowed func(actionID string) bool) error {
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM admin_commands WHERE status IN ('accepted','running','waiting_verification') ORDER BY created_at,id`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		var err error
		for attempt := 0; attempt < 9; attempt++ {
			if allowed != nil {
				command, loadErr := l.AdminCommand(ctx, id)
				if loadErr != nil {
					return loadErr
				}
				if !allowed(command.ActionID) {
					var revoked bool
					revoked, err = l.AdminRevokeUnstarted(ctx, id)
					if err == nil && revoked {
						break
					}
					if err == nil {
						var mayResume bool
						mayResume, err = l.adminMayResumeRevoked(ctx, command)
						if err == nil && !mayResume {
							err = l.adminMarkRevokedHold(ctx, id)
							if err == nil {
								break
							}
						}
					}
				}
			}
			if err == nil {
				_, err = l.AdminExecuteCommand(ctx, id)
			}
			if !errors.Is(err, ErrAdminLeaseBusy) && !errors.Is(err, ErrAdminLeaseLost) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		if errors.Is(err, ErrAdminFaultOwnedByOtherCommand) {
			// The ticket's original command remains in this resume batch.
			continue
		}
		if err != nil {
			return fmt.Errorf("resume admin command %s: %w", id, err)
		}
	}
	return nil
}
