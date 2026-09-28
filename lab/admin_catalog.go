package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type adminProPricePayload struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	FixedMinor    string `json:"fixed_minor"`
	SeatMinor     string `json:"seat_minor"`
	IncludedTasks string `json:"included_tasks"`
	UsageRateNum  string `json:"usage_rate_num"`
	UsageRateDen  string `json:"usage_rate_den"`
	EffectiveFrom string `json:"effective_from"`
}

type adminMeterPayload struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	Unit          string `json:"unit"`
	SchemaVersion string `json:"schema_version"`
}

type adminMeteredPricePayload struct {
	ID               string `json:"id"`
	PlanID           string `json:"plan_id"`
	Version          string `json:"version"`
	FixedMinor       string `json:"fixed_minor"`
	SeatMinor        string `json:"seat_minor"`
	MeterID          string `json:"meter_id"`
	IncludedQuantity string `json:"included_quantity"`
	UsageRateNum     string `json:"usage_rate_num"`
	UsageRateDen     string `json:"usage_rate_den"`
	EffectiveFrom    string `json:"effective_from"`
}

type adminSelectionPayload struct {
	PlanID         string `json:"plan_id"`
	Cohort         string `json:"cohort"`
	EffectiveAt    string `json:"effective_at"`
	PriceVersionID string `json:"price_version_id"`
}

func canonicalCatalogInt(value string, min int64) (string, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < min {
		return "", ErrAdminInvalidCommand
	}
	return strconv.FormatInt(n, 10), nil
}

func canonicalCatalogUTC(value string) (string, error) {
	return canonicalAdminUTC(value)
}

func minimumUpfrontTextFits(fixedText, seatText string) bool {
	fixed, fixedErr := strconv.ParseInt(fixedText, 10, 64)
	seat, seatErr := strconv.ParseInt(seatText, 10, 64)
	return fixedErr == nil && seatErr == nil && minimumUpfrontFits(fixed, seat)
}

func canonicalAdminCatalogPayload(actionID string, raw json.RawMessage) ([]byte, error) {
	var err error
	switch actionID {
	case "C18":
		var p adminProPricePayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			return nil, ErrAdminInvalidCommand
		}
		if p.Version, err = canonicalCatalogInt(p.Version, 1); err != nil {
			return nil, err
		}
		if p.FixedMinor, err = canonicalCatalogInt(p.FixedMinor, 1); err != nil {
			return nil, err
		}
		if p.SeatMinor, err = canonicalCatalogInt(p.SeatMinor, 1); err != nil {
			return nil, err
		}
		if !minimumUpfrontTextFits(p.FixedMinor, p.SeatMinor) {
			return nil, ErrAdminInvalidCommand
		}
		if p.IncludedTasks, err = canonicalCatalogInt(p.IncludedTasks, 0); err != nil {
			return nil, err
		}
		if p.UsageRateNum, err = canonicalCatalogInt(p.UsageRateNum, 0); err != nil {
			return nil, err
		}
		if p.UsageRateDen, err = canonicalCatalogInt(p.UsageRateDen, 1); err != nil {
			return nil, err
		}
		if p.EffectiveFrom, err = canonicalCatalogUTC(p.EffectiveFrom); err != nil {
			return nil, err
		}
		return json.Marshal(p)
	case "C19":
		var p adminMeterPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.ID == "" || p.Source == "" || p.Unit == "" {
			return nil, ErrAdminInvalidCommand
		}
		if p.SchemaVersion, err = canonicalCatalogInt(p.SchemaVersion, 1); err != nil {
			return nil, err
		}
		return json.Marshal(p)
	case "C20":
		var p adminMeteredPricePayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.ID == "" || p.PlanID == "" || p.MeterID == "" {
			return nil, ErrAdminInvalidCommand
		}
		if p.Version, err = canonicalCatalogInt(p.Version, 1); err != nil {
			return nil, err
		}
		if p.FixedMinor, err = canonicalCatalogInt(p.FixedMinor, 1); err != nil {
			return nil, err
		}
		if p.SeatMinor, err = canonicalCatalogInt(p.SeatMinor, 0); err != nil {
			return nil, err
		}
		if !minimumUpfrontTextFits(p.FixedMinor, p.SeatMinor) {
			return nil, ErrAdminInvalidCommand
		}
		if p.IncludedQuantity, err = canonicalCatalogInt(p.IncludedQuantity, 1); err != nil {
			return nil, err
		}
		if p.UsageRateNum, err = canonicalCatalogInt(p.UsageRateNum, 1); err != nil {
			return nil, err
		}
		if p.UsageRateDen, err = canonicalCatalogInt(p.UsageRateDen, 1); err != nil {
			return nil, err
		}
		if p.EffectiveFrom, err = canonicalCatalogUTC(p.EffectiveFrom); err != nil {
			return nil, err
		}
		return json.Marshal(p)
	case "C21":
		var p adminSelectionPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.PlanID == "" || p.Cohort == "" || p.PriceVersionID == "" {
			return nil, ErrAdminInvalidCommand
		}
		if p.EffectiveAt, err = canonicalCatalogUTC(p.EffectiveAt); err != nil {
			return nil, err
		}
		return json.Marshal(p)
	default:
		return nil, ErrAdminUnsupportedAction
	}
}

func parseProPrice(p adminProPricePayload) ProPriceSpec {
	version, _ := strconv.ParseInt(p.Version, 10, 64)
	fixed, _ := strconv.ParseInt(p.FixedMinor, 10, 64)
	seat, _ := strconv.ParseInt(p.SeatMinor, 10, 64)
	included, _ := strconv.ParseInt(p.IncludedTasks, 10, 64)
	num, _ := strconv.ParseInt(p.UsageRateNum, 10, 64)
	den, _ := strconv.ParseInt(p.UsageRateDen, 10, 64)
	at, _ := time.Parse(time.RFC3339Nano, p.EffectiveFrom)
	return ProPriceSpec{ID: p.ID, Version: version, FixedMinor: fixed, SeatMinor: seat, IncludedTasks: included, UsageRateNum: num, UsageRateDen: den, EffectiveFrom: at}
}

func parseMeteredPrice(p adminMeteredPricePayload) MeteredPriceSpec {
	version, _ := strconv.ParseInt(p.Version, 10, 64)
	fixed, _ := strconv.ParseInt(p.FixedMinor, 10, 64)
	seat, _ := strconv.ParseInt(p.SeatMinor, 10, 64)
	included, _ := strconv.ParseInt(p.IncludedQuantity, 10, 64)
	num, _ := strconv.ParseInt(p.UsageRateNum, 10, 64)
	den, _ := strconv.ParseInt(p.UsageRateDen, 10, 64)
	at, _ := time.Parse(time.RFC3339Nano, p.EffectiveFrom)
	return MeteredPriceSpec{ID: p.ID, PlanID: p.PlanID, Version: version, FixedMinor: fixed, SeatMinor: seat, MeterID: p.MeterID, IncludedQuantity: included, UsageRateNum: num, UsageRateDen: den, EffectiveFrom: at}
}

type adminCatalogSnapshot struct {
	Sources []byte
	Impact  []byte
}

func (l *Lab) loadAdminCatalogSnapshot(ctx context.Context, tx *sql.Tx, actionID string, canonical []byte) (adminCatalogSnapshot, error) {
	var snapshot adminCatalogSnapshot
	source := map[string]string{}
	impact := map[string]string{}
	switch actionID {
	case "C18":
		var p adminProPricePayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		spec := parseProPrice(p)
		if !minimumUpfrontFits(spec.FixedMinor, spec.SeatMinor) {
			return snapshot, ErrAdminInvalidCommand
		}
		checksum := hash(spec.ID, spec.Version, "USD", spec.FixedMinor, "seats", spec.SeatMinor, "tasks", spec.IncludedTasks, spec.UsageRateNum, spec.UsageRateDen, spec.EffectiveFrom.UTC().UnixNano())
		var savedChecksum, state string
		err := tx.QueryRowContext(ctx, `SELECT checksum,publication_state FROM price_versions WHERE id=?`, p.ID).Scan(&savedChecksum, &state)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return snapshot, err
		}
		if err == nil && (savedChecksum != checksum || state != "published") {
			return snapshot, ErrConflict
		}
		var occupant string
		err = tx.QueryRowContext(ctx, `SELECT id FROM price_versions WHERE plan_id='pro' AND version=?`, spec.Version).Scan(&occupant)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return snapshot, err
		}
		if occupant != "" && occupant != p.ID {
			return snapshot, ErrConflict
		}
		source["existing_checksum"], source["existing_state"], source["version_occupant"] = savedChecksum, state, occupant
		impact["id"], impact["plan_id"], impact["version"], impact["checksum"] = p.ID, "pro", p.Version, checksum
		impact["fixed_minor"], impact["seat_minor"], impact["included_tasks"] = p.FixedMinor, p.SeatMinor, p.IncludedTasks
		impact["currency"] = "USD"
		impact["usage_rate_num"], impact["usage_rate_den"], impact["effective_from"] = p.UsageRateNum, p.UsageRateDen, p.EffectiveFrom
	case "C19":
		var p adminMeterPayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		var savedSource, savedUnit string
		var savedVersion int64
		err := tx.QueryRowContext(ctx, `SELECT source,unit,schema_version FROM meter_schemas WHERE id=?`, p.ID).Scan(&savedSource, &savedUnit, &savedVersion)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return snapshot, err
		}
		if err == nil && (savedSource != p.Source || savedUnit != p.Unit || strconv.FormatInt(savedVersion, 10) != p.SchemaVersion) {
			return snapshot, ErrConflict
		}
		source["existing_source"], source["existing_unit"] = savedSource, savedUnit
		if err == nil {
			source["existing_schema_version"] = strconv.FormatInt(savedVersion, 10)
		}
		impact["id"], impact["source"], impact["unit"], impact["schema_version"] = p.ID, p.Source, p.Unit, p.SchemaVersion
	case "C20":
		var p adminMeteredPricePayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		spec := parseMeteredPrice(p)
		if !minimumUpfrontFits(spec.FixedMinor, spec.SeatMinor) {
			return snapshot, ErrAdminInvalidCommand
		}
		var meterSource, meterUnit string
		var meterVersion int64
		if err := tx.QueryRowContext(ctx, `SELECT source,unit,schema_version FROM meter_schemas WHERE id=?`, p.MeterID).Scan(&meterSource, &meterUnit, &meterVersion); err != nil {
			return snapshot, err
		}
		checksum := hash(spec.ID, spec.PlanID, spec.Version, "USD", spec.FixedMinor, spec.SeatMinor, spec.MeterID, spec.IncludedQuantity, spec.UsageRateNum, spec.UsageRateDen, spec.EffectiveFrom.UTC().UnixNano())
		var savedChecksum, state string
		err := tx.QueryRowContext(ctx, `SELECT checksum,publication_state FROM price_versions WHERE id=?`, p.ID).Scan(&savedChecksum, &state)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return snapshot, err
		}
		if err == nil && (savedChecksum != checksum || state != "published") {
			return snapshot, ErrConflict
		}
		var occupant string
		err = tx.QueryRowContext(ctx, `SELECT id FROM price_versions WHERE plan_id=? AND version=?`, p.PlanID, spec.Version).Scan(&occupant)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return snapshot, err
		}
		if occupant != "" && occupant != p.ID {
			return snapshot, ErrConflict
		}
		source["meter_source"], source["meter_unit"], source["meter_schema_version"] = meterSource, meterUnit, strconv.FormatInt(meterVersion, 10)
		source["existing_checksum"], source["existing_state"], source["version_occupant"] = savedChecksum, state, occupant
		impact["id"], impact["plan_id"], impact["version"], impact["checksum"] = p.ID, p.PlanID, p.Version, checksum
		impact["fixed_minor"], impact["seat_minor"], impact["meter_id"] = p.FixedMinor, p.SeatMinor, p.MeterID
		impact["currency"] = "USD"
		impact["included_quantity"], impact["usage_rate_num"], impact["usage_rate_den"] = p.IncludedQuantity, p.UsageRateNum, p.UsageRateDen
		impact["effective_from"] = p.EffectiveFrom
	case "C21":
		var p adminSelectionPayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		effective, err := time.Parse(time.RFC3339Nano, p.EffectiveAt)
		if err != nil {
			return snapshot, err
		}
		var pricePlan, state, checksum string
		var from int64
		var to sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT plan_id,publication_state,checksum,effective_from,effective_to FROM price_versions WHERE id=?`, p.PriceVersionID).Scan(&pricePlan, &state, &checksum, &from, &to); err != nil {
			return snapshot, err
		}
		at := effective.UnixNano()
		if pricePlan != p.PlanID || state != "published" || at < from || (to.Valid && at >= to.Int64) {
			return snapshot, ErrConflict
		}
		var occupant string
		err = tx.QueryRowContext(ctx, `SELECT price_version_id FROM catalog_selection WHERE plan_id=? AND cohort=? AND effective_at=?`, p.PlanID, p.Cohort, at).Scan(&occupant)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return snapshot, err
		}
		if occupant != "" && occupant != p.PriceVersionID {
			return snapshot, ErrConflict
		}
		source["price_version_id"], source["price_checksum"], source["price_state"] = p.PriceVersionID, checksum, state
		source["price_effective_from"], source["price_effective_to"], source["selection_occupant"] = strconv.FormatInt(from, 10), "", occupant
		if to.Valid {
			source["price_effective_to"] = strconv.FormatInt(to.Int64, 10)
		}
		impact["plan_id"], impact["cohort"], impact["effective_at"], impact["price_version_id"] = p.PlanID, p.Cohort, p.EffectiveAt, p.PriceVersionID
	default:
		return snapshot, ErrAdminUnsupportedAction
	}
	var err error
	snapshot.Sources, err = json.Marshal(source)
	if err != nil {
		return adminCatalogSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(impact)
	return snapshot, err
}

func (l *Lab) adminCreateCatalogPreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID != "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadAdminCatalogSnapshot(ctx, tx, actionID, canonical)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, "", adminIntentHash(actionID, "", canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: "", ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeAdminCatalogTx(ctx context.Context, tx *sql.Tx, commandID, previewID, actionID, payloadJSON, businessTime string) (any, error) {
	var storedSources string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	snapshot, err := l.loadAdminCatalogSnapshot(ctx, tx, actionID, []byte(payloadJSON))
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	switch actionID {
	case "C18":
		var p adminProPricePayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		terms, err := l.publishProPriceTx(ctx, tx, at, parseProPrice(p))
		if err != nil {
			return nil, err
		}
		return map[string]string{"price_version_id": p.ID, "plan_id": "pro", "checksum": terms.Checksum}, nil
	case "C19":
		var p adminMeterPayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		version, _ := strconv.ParseInt(p.SchemaVersion, 10, 64)
		if err := l.registerMeterTx(ctx, tx, MeterSpec{ID: p.ID, Source: p.Source, Unit: p.Unit, SchemaVersion: version}); err != nil {
			return nil, err
		}
		return map[string]string{"meter_id": p.ID, "schema_version": p.SchemaVersion}, nil
	case "C20":
		var p adminMeteredPricePayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		terms, err := l.publishMeteredPriceTx(ctx, tx, at, parseMeteredPrice(p))
		if err != nil {
			return nil, err
		}
		return map[string]string{"price_version_id": p.ID, "plan_id": p.PlanID, "checksum": terms.Checksum}, nil
	case "C21":
		var p adminSelectionPayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		effective, err := time.Parse(time.RFC3339Nano, p.EffectiveAt)
		if err != nil {
			return nil, err
		}
		if err := l.selectCatalogPriceTx(ctx, tx, p.PlanID, p.Cohort, effective, p.PriceVersionID); err != nil {
			return nil, err
		}
		return map[string]string{"plan_id": p.PlanID, "cohort": p.Cohort, "effective_at": p.EffectiveAt, "price_version_id": p.PriceVersionID}, nil
	default:
		return nil, ErrAdminUnsupportedAction
	}
}
