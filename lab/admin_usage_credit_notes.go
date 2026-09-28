package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

type usageCreditNotePreviewItem struct {
	InvoiceID           string `json:"invoice_id"`
	AmountMinor         string `json:"amount_minor"`
	ApplicationMinor    string `json:"application_minor"`
	AlreadyNotedMinor   string `json:"already_noted_minor"`
	ReductionSourceJSON string `json:"reduction_source_json"`
}
type usageCreditNotePreviewPeriod struct {
	SubscriptionID string                       `json:"subscription_id"`
	PeriodIndex    int                          `json:"period_index"`
	BilledMinor    string                       `json:"billed_minor"`
	CreditedMinor  string                       `json:"credited_minor"`
	RatedMinor     string                       `json:"rated_minor"`
	Items          []usageCreditNotePreviewItem `json:"items"`
}

func (l *Lab) loadUsageCreditNotePreviewPeriod(ctx context.Context, tx *sql.Tx, subID string, index int) (usageCreditNotePreviewPeriod, error) {
	period := usageCreditNotePreviewPeriod{SubscriptionID: subID, PeriodIndex: index, Items: []usageCreditNotePreviewItem{}}
	var billed, credited, rated int64
	if err := tx.QueryRowContext(ctx, `SELECT billed_minor,credited_minor,rated_minor FROM usage_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&billed, &credited, &rated); err != nil {
		return period, err
	}
	needed := billed - credited - rated
	if needed <= 0 {
		return period, ErrConflict
	}
	period.BilledMinor, period.CreditedMinor, period.RatedMinor = strconv.FormatInt(billed, 10), strconv.FormatInt(credited, 10), strconv.FormatInt(rated, 10)
	rows, err := tx.QueryContext(ctx, `SELECT a.invoice_id,a.amount_minor,COALESCE((SELECT SUM(n.amount_minor) FROM usage_credit_notes n WHERE n.subscription_id=a.subscription_id AND n.period_index=a.period_index AND n.source_invoice_id=a.invoice_id),0) FROM usage_invoice_applications a WHERE a.subscription_id=? AND a.period_index=? ORDER BY a.applied_at DESC,a.invoice_id DESC`, subID, index)
	if err != nil {
		return period, err
	}
	type application struct {
		invoice       string
		amount, noted int64
	}
	var applications []application
	for rows.Next() {
		var a application
		if err := rows.Scan(&a.invoice, &a.amount, &a.noted); err != nil {
			rows.Close()
			return period, err
		}
		applications = append(applications, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return period, err
	}
	for _, a := range applications {
		available := a.amount - a.noted
		if available <= 0 {
			continue
		}
		amount := needed
		if available < amount {
			amount = available
		}
		reduction, err := l.loadReductionSnapshot(ctx, tx, a.invoice, amount, false)
		if err != nil {
			return period, err
		}
		period.Items = append(period.Items, usageCreditNotePreviewItem{InvoiceID: a.invoice, AmountMinor: strconv.FormatInt(amount, 10), ApplicationMinor: strconv.FormatInt(a.amount, 10), AlreadyNotedMinor: strconv.FormatInt(a.noted, 10), ReductionSourceJSON: string(reduction.Sources)})
		needed -= amount
		if needed == 0 {
			break
		}
	}
	if needed != 0 {
		return period, ErrConflict
	}
	return period, nil
}

func (l *Lab) adminCreateUsageCreditNotesPreview(ctx context.Context, actorID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID != "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	previousSource, err := lastCommittedBatchSource(ctx, tx, "C30")
	if err != nil {
		return AdminPreview{}, err
	}
	type key struct {
		sub   string
		index int
	}
	var cursor key
	if previousSource != "" {
		var previous map[string]string
		if err := json.Unmarshal([]byte(previousSource), &previous); err != nil {
			return AdminPreview{}, err
		}
		var periods []usageCreditNotePreviewPeriod
		if err := json.Unmarshal([]byte(previous["periods_json"]), &periods); err != nil || len(periods) == 0 {
			return AdminPreview{}, ErrAdminPreviewStale
		}
		last := periods[len(periods)-1]
		cursor = key{sub: last.SubscriptionID, index: last.PeriodIndex}
	}
	var keys []key
	readCandidates := func(query string) error {
		rows, err := tx.QueryContext(ctx, query, cursor.sub, cursor.sub, cursor.index, 101-len(keys))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k key
			if err := rows.Scan(&k.sub, &k.index); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	}
	if err := readCandidates(`SELECT subscription_id,period_index FROM usage_periods WHERE billed_minor-credited_minor>rated_minor AND (subscription_id>? OR (subscription_id=? AND period_index>?)) ORDER BY subscription_id,period_index LIMIT ?`); err != nil {
		return AdminPreview{}, err
	}
	if cursor.sub != "" && len(keys) <= 100 {
		if err := readCandidates(`SELECT subscription_id,period_index FROM usage_periods WHERE billed_minor-credited_minor>rated_minor AND (subscription_id<? OR (subscription_id=? AND period_index<=?)) ORDER BY subscription_id,period_index LIMIT ?`); err != nil {
			return AdminPreview{}, err
		}
	}
	if len(keys) == 0 {
		return AdminPreview{}, ErrConflict
	}
	hasMore := len(keys) > 100
	if hasMore {
		keys = keys[:100]
	}
	periods := make([]usageCreditNotePreviewPeriod, 0, len(keys))
	var total int64
	var count int
	var names []string
	for _, k := range keys {
		period, err := l.loadUsageCreditNotePreviewPeriod(ctx, tx, k.sub, k.index)
		if err != nil {
			return AdminPreview{}, err
		}
		for _, item := range period.Items {
			amount, _ := strconv.ParseInt(item.AmountMinor, 10, 64)
			if amount <= 0 || amount > math.MaxInt64-total {
				return AdminPreview{}, ErrConflict
			}
			total += amount
			count++
		}
		names = append(names, k.sub+":"+strconv.Itoa(k.index))
		periods = append(periods, period)
	}
	periodJSON, err := json.Marshal(periods)
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(map[string]string{"periods_json": string(periodJSON)})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"period_count": strconv.Itoa(len(periods)), "has_more_candidates": strconv.FormatBool(hasMore), "note_count": strconv.Itoa(count), "periods": strings.Join(names, ", "), "total_reduction_minor": strconv.FormatInt(total, 10), "items_json": string(periodJSON)})
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C30", "", adminIntentHash("C30", "", canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C30", TargetID: "", ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}
