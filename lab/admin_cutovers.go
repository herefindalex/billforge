package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type adminCutoverPayload struct {
	MaxQuoteP95Millis    string `json:"max_quote_p95_millis"`
	MaxUnknownPayments   string `json:"max_unknown_payments"`
	MaxOpenDiscrepancies string `json:"max_open_discrepancies"`
}

type adminStopMigrationPayload struct {
	Reason string `json:"reason"`
}

type adminCutoverSource struct {
	Link      AccountLink        `json:"link"`
	Readiness MigrationReadiness `json:"readiness"`
}

func canonicalAdminCutoverPayload(actionID string, raw json.RawMessage) ([]byte, error) {
	if actionID == "C43" {
		var p adminStopMigrationPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		p.Reason = strings.TrimSpace(p.Reason)
		if p.Reason == "" {
			return nil, ErrAdminInvalidCommand
		}
		return json.Marshal(p)
	}
	var p adminCutoverPayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	for i, value := range []*string{&p.MaxQuoteP95Millis, &p.MaxUnknownPayments, &p.MaxOpenDiscrepancies} {
		n, err := strconv.ParseInt(*value, 10, 64)
		if err != nil || n < 0 || (i == 0 && n == 0) {
			return nil, ErrAdminInvalidCommand
		}
		*value = strconv.FormatInt(n, 10)
	}
	return json.Marshal(p)
}

func parseAdminCutoverPayload(canonical []byte) (MigrationThresholds, error) {
	var p adminCutoverPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return MigrationThresholds{}, err
	}
	quote, _ := strconv.ParseInt(p.MaxQuoteP95Millis, 10, 64)
	unknown, _ := strconv.ParseInt(p.MaxUnknownPayments, 10, 64)
	open, _ := strconv.ParseInt(p.MaxOpenDiscrepancies, 10, 64)
	return MigrationThresholds{MaxQuoteP95Millis: quote, MaxUnknownPayments: unknown, MaxOpenDiscrepancies: open}, nil
}

func (l *Lab) adminCreateCutoverPreview(ctx context.Context, actorID, actionID, accountID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || accountID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	link, err := loadAccountLink(ctx, tx, accountID)
	if err != nil {
		_ = tx.Rollback()
		return AdminPreview{}, err
	}
	if link.Stopped {
		_ = tx.Rollback()
		return AdminPreview{}, ErrConflict
	}
	source := adminCutoverSource{Link: link}
	var outcome AccountLink
	if actionID == "C43" {
		var p adminStopMigrationPayload
		_ = json.Unmarshal(canonical, &p)
		outcome, err = l.stopAccountMigrationTx(ctx, tx, accountID, p.Reason)
	} else {
		limits, parseErr := parseAdminCutoverPayload(canonical)
		if parseErr != nil {
			_ = tx.Rollback()
			return AdminPreview{}, parseErr
		}
		source.Readiness, err = l.migrationReadiness(ctx, tx, accountID, limits)
		if err == nil {
			if actionID == "C41" {
				outcome, err = l.switchAccountReadTx(ctx, tx, accountID, limits)
			} else {
				outcome, err = l.switchAccountWriterTx(ctx, tx, accountID, limits)
			}
		}
	}
	_ = tx.Rollback()
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(source)
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"legacy_account_id": accountID, "old_read_owner": link.ReadOwner, "old_writer_owner": link.WriterOwner, "new_read_owner": outcome.ReadOwner, "new_writer_owner": outcome.WriterOwner, "stopped": pBool(outcome.Stopped)})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, accountID, adminIntentHash(actionID, accountID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: accountID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeAdminCutoverTx(ctx context.Context, tx *sql.Tx, commandID, actorID, actionID, accountID, previewID string, canonical []byte) (any, error) {
	var savedIntent, savedSources, expiry string
	err := tx.QueryRowContext(ctx, `SELECT intent_json,source_versions_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id=? AND target_id=? AND claimed_command_id=?`, previewID, actorID, actionID, accountID, commandID).Scan(&savedIntent, &savedSources, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !time.Now().UTC().Before(expires) || savedIntent != string(canonical) {
		return nil, ErrAdminPreviewStale
	}
	var expected adminCutoverSource
	if err := json.Unmarshal([]byte(savedSources), &expected); err != nil {
		return nil, err
	}
	current, err := loadAccountLink(ctx, tx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if current != expected.Link {
		return nil, ErrAdminPreviewStale
	}
	var outcome AccountLink
	if actionID == "C43" {
		var p adminStopMigrationPayload
		_ = json.Unmarshal(canonical, &p)
		outcome, err = l.stopAccountMigrationTx(ctx, tx, accountID, p.Reason)
	} else {
		limits, parseErr := parseAdminCutoverPayload(canonical)
		if parseErr != nil {
			return nil, parseErr
		}
		readiness, checkErr := l.migrationReadiness(ctx, tx, accountID, limits)
		if checkErr != nil {
			return nil, checkErr
		}
		if readiness != expected.Readiness {
			return nil, ErrAdminPreviewStale
		}
		if actionID == "C41" {
			outcome, err = l.switchAccountReadTx(ctx, tx, accountID, limits)
		} else {
			outcome, err = l.switchAccountWriterTx(ctx, tx, accountID, limits)
		}
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"legacy_account_id": accountID, "read_owner": outcome.ReadOwner, "writer_owner": outcome.WriterOwner, "stopped": pBool(outcome.Stopped)}, nil
}
