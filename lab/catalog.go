package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

type PriceTerms struct {
	PriceVersionID   string
	Currency         string
	FixedMinor       int64
	SeatMinor        int64
	MeterID          string
	IncludedQuantity int64
	IncludedTasks    int64
	UsageRateNum     int64 // minor units per task, rational numerator
	UsageRateDen     int64
	Checksum         string
}

// Prices with per-seat charges must support the minimum saleable quantity.
// Larger quantities are checked again when a quote is created.
func minimumUpfrontFits(fixedMinor, seatMinor int64) bool {
	return fixedMinor > 0 && seatMinor >= 0 && seatMinor <= math.MaxInt64-fixedMinor
}

type priceQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func migrateCatalog(db *sql.DB) error {
	for _, column := range []struct{ table, name, ddl string }{
		{"price_versions", "publication_state", `ALTER TABLE price_versions ADD COLUMN publication_state TEXT NOT NULL DEFAULT 'published' CHECK(publication_state IN ('draft','published'))`},
		{"price_versions", "effective_from", `ALTER TABLE price_versions ADD COLUMN effective_from INTEGER NOT NULL DEFAULT 0`},
		{"price_versions", "effective_to", `ALTER TABLE price_versions ADD COLUMN effective_to INTEGER`},
		{"quotes", "seat_quantity", `ALTER TABLE quotes ADD COLUMN seat_quantity INTEGER NOT NULL DEFAULT 0 CHECK(seat_quantity>=0)`},
		{"subscriptions", "seat_quantity", `ALTER TABLE subscriptions ADD COLUMN seat_quantity INTEGER NOT NULL DEFAULT 0 CHECK(seat_quantity>=0)`},
		{"subscriptions", "revision", `ALTER TABLE subscriptions ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0)`},
	} {
		if err := ensureCatalogColumn(db, column.table, column.name, column.ddl); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS price_immutable_update;
CREATE TRIGGER price_immutable_update BEFORE UPDATE ON price_versions
WHEN OLD.publication_state='published'
BEGIN SELECT RAISE(ABORT,'published price immutable'); END;
CREATE TABLE IF NOT EXISTS price_components (
 price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 component_code TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('per_seat','included_quantity','usage_overage')),
 amount_minor INTEGER NOT NULL DEFAULT 0 CHECK(amount_minor>=0),
 quantity INTEGER NOT NULL DEFAULT 0 CHECK(quantity>=0),
 rate_num INTEGER NOT NULL DEFAULT 0 CHECK(rate_num>=0),
 rate_den INTEGER NOT NULL DEFAULT 1 CHECK(rate_den>0),
 meter_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(price_version_id,component_code));
CREATE TRIGGER IF NOT EXISTS price_component_insert_draft BEFORE INSERT ON price_components
WHEN (SELECT publication_state FROM price_versions WHERE id=NEW.price_version_id)!='draft'
BEGIN SELECT RAISE(ABORT,'published price components immutable'); END;
CREATE TRIGGER IF NOT EXISTS price_component_update_draft BEFORE UPDATE ON price_components
WHEN (SELECT publication_state FROM price_versions WHERE id=OLD.price_version_id)!='draft'
BEGIN SELECT RAISE(ABORT,'published price components immutable'); END;
CREATE TRIGGER IF NOT EXISTS price_component_delete_draft BEFORE DELETE ON price_components
WHEN (SELECT publication_state FROM price_versions WHERE id=OLD.price_version_id)!='draft'
BEGIN SELECT RAISE(ABORT,'published price components immutable'); END;`); err != nil {
		return err
	}
	// Seed the fixed, seat and usage example as one transaction. A crash cannot
	// expose a published Pro version with only some of its components.
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRow(`SELECT publication_state FROM price_versions WHERE id='pro-v1'`).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state)
VALUES('pro-v1','pro',1,'USD',5000,0,'','draft')`); err != nil {
			return err
		}
		for _, component := range []struct {
			code, kind, meter          string
			amount, quantity, num, den int64
		}{
			{"seats", "per_seat", "", 1000, 0, 0, 1},
			{"tasks_included", "included_quantity", "tasks", 0, 20000, 0, 1},
			{"tasks_overage", "usage_overage", "tasks", 0, 0, 1, 10},
		} {
			if _, err := tx.Exec(`INSERT INTO price_components(price_version_id,component_code,kind,amount_minor,quantity,rate_num,rate_den,meter_id) VALUES('pro-v1',?,?,?,?,?,?,?)`, component.code, component.kind, component.amount, component.quantity, component.num, component.den, component.meter); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE price_versions SET publication_state='published',checksum=? WHERE id='pro-v1' AND publication_state='draft'`, hash("pro-v1", 1, "USD", 5000, "seats", 1000, "tasks", 20000, 1, 10)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if existing != "published" {
		return errors.New("reserved pro-v1 price version is not published")
	}
	if _, err := tx.Exec(`INSERT INTO catalog_selection(plan_id,cohort,effective_at,price_version_id) VALUES('pro','default',0,'pro-v1') ON CONFLICT(plan_id,cohort,effective_at) DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureCatalogColumn(db *sql.DB, table, name, ddl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var columnName, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &columnName, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || columnName == name
	}
	err = rows.Err()
	rows.Close()
	if err != nil || found {
		return err
	}
	_, err = db.Exec(ddl)
	return err
}

func loadPriceTerms(ctx context.Context, q priceQuerier, priceVersionID string) (PriceTerms, error) {
	p := PriceTerms{PriceVersionID: priceVersionID, UsageRateDen: 1}
	var state string
	err := q.QueryRowContext(ctx, `SELECT currency,fixed_amount_minor,checksum,publication_state FROM price_versions WHERE id=?`, priceVersionID).
		Scan(&p.Currency, &p.FixedMinor, &p.Checksum, &state)
	if err != nil {
		return PriceTerms{}, err
	}
	if state != "published" {
		return PriceTerms{}, ErrConflict
	}
	rows, err := q.QueryContext(ctx, `SELECT component_code,kind,amount_minor,quantity,rate_num,rate_den,meter_id FROM price_components WHERE price_version_id=? ORDER BY component_code`, priceVersionID)
	if err != nil {
		return PriceTerms{}, err
	}
	defer rows.Close()
	var hasAllowance, hasOverage bool
	for rows.Next() {
		var code, kind, meter string
		var amount, quantity, num, den int64
		if err := rows.Scan(&code, &kind, &amount, &quantity, &num, &den, &meter); err != nil {
			return PriceTerms{}, err
		}
		switch kind {
		case "per_seat":
			if code != "seats" || amount <= 0 || p.SeatMinor != 0 {
				return PriceTerms{}, fmt.Errorf("unsupported per-seat component %q", code)
			}
			p.SeatMinor = amount
		case "included_quantity":
			if meter == "" || hasAllowance || quantity < 0 || (p.MeterID != "" && p.MeterID != meter) {
				return PriceTerms{}, fmt.Errorf("unsupported allowance component %q", code)
			}
			hasAllowance = true
			p.MeterID, p.IncludedQuantity = meter, quantity
			if meter == "tasks" {
				p.IncludedTasks = quantity
			}
		case "usage_overage":
			if meter == "" || num <= 0 || den <= 0 || hasOverage || (p.MeterID != "" && p.MeterID != meter) {
				return PriceTerms{}, fmt.Errorf("unsupported usage component %q", code)
			}
			hasOverage = true
			p.MeterID = meter
			p.UsageRateNum, p.UsageRateDen = num, den
		default:
			return PriceTerms{}, fmt.Errorf("unsupported component kind %q", kind)
		}
	}
	if err := rows.Err(); err != nil {
		return PriceTerms{}, err
	}
	if hasAllowance != hasOverage {
		return PriceTerms{}, ErrConflict
	}
	return p, nil
}

func (p PriceTerms) UpfrontMinor(seats int64) (int64, error) {
	if (p.SeatMinor == 0 && seats != 0) || (p.SeatMinor > 0 && seats < 1) || seats < 0 {
		return 0, ErrConflict
	}
	if p.SeatMinor > 0 && seats > (math.MaxInt64-p.FixedMinor)/p.SeatMinor {
		return 0, errors.New("price amount overflow")
	}
	return p.FixedMinor + p.SeatMinor*seats, nil
}
