package lab

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type adminResourceSpec struct {
	table     string
	selectSQL string
	filter    string
}

// Only these columns may be used as list filters. Keep the SQL identifiers
// here, never in a request payload.
var adminResourceFilterColumns = map[string]map[string]string{
	"prices":              {"id_prefix": "id", "plan_id": "plan_id"},
	"catalog-selections":  {"plan_id": "plan_id", "cohort": "cohort", "price_version_id": "price_version_id"},
	"price-migrations":    {"id_prefix": "id", "cohort": "cohort", "status": "status", "created_from": "created_at", "created_before": "created_at"},
	"contracts":           {"id_prefix": "id", "customer_id": "customer_id"},
	"usage-events":        {"subscription_id": "subscription_id", "period_index": "period_index", "source": "source"},
	"usage-periods":       {"subscription_id": "subscription_id"},
	"quotes":              {"id_prefix": "id", "customer_id": "customer_id"},
	"subscriptions":       {"id_prefix": "id", "customer_id": "customer_id", "status": "status", "created_from": "created_at", "created_before": "created_at"},
	"invoices":            {"id_prefix": "id", "subscription_id": "subscription_id"},
	"payments":            {"id_prefix": "id", "invoice_id": "invoice_id", "status": "status"},
	"immediate-changes":   {"id_prefix": "id", "subscription_id": "subscription_id", "status": "status"},
	"credits":             {"id_prefix": "id", "source_invoice_id": "source_invoice_id", "created_from": "created_at", "created_before": "created_at"},
	"refunds":             {"id_prefix": "id", "grant_id": "grant_id", "status": "status", "created_from": "created_at", "created_before": "created_at"},
	"outbox":              {"id_prefix": "id", "kind": "kind"},
	"meters":              {"id_prefix": "id", "source": "source"},
	"account-migrations":  {"id_prefix": "legacy_account_id", "customer_id": "customer_id", "cohort": "cohort", "created_from": "created_at", "created_before": "created_at"},
	"legacy-provenance":   {"id_prefix": "legacy_invoice_id", "legacy_account_id": "legacy_account_id", "status": "status"},
	"reconciliation-runs": {"id_prefix": "id", "created_from": "created_at", "created_before": "created_at"},
	"discrepancies":       {"id_prefix": "id", "object_id": "object_id", "status": "status", "kind": "kind"},
}

var adminResourceSpecs = map[string]adminResourceSpec{
	"prices":              {table: "price_versions", selectSQL: `t.rowid,t.*,COALESCE((SELECT amount_minor FROM price_components c WHERE c.price_version_id=t.id AND c.component_code='seats'),0) AS seat_minor`},
	"catalog-selections":  {table: "catalog_selection"},
	"price-migrations":    {table: "price_migrations"},
	"contracts":           {table: "contract_versions"},
	"usage-events":        {table: "usage_events"},
	"usage-periods":       {table: "usage_periods"},
	"quotes":              {table: "quotes", selectSQL: `q.rowid,q.*,EXISTS(SELECT 1 FROM subscriptions s WHERE s.quote_id=q.id) AS accepted`},
	"subscriptions":       {table: "subscriptions", selectSQL: `s.rowid,s.*,(SELECT status FROM entitlements e WHERE e.subscription_id=s.id) AS entitlement_status,(SELECT reason FROM entitlements e WHERE e.subscription_id=s.id) AS entitlement_reason`},
	"invoices":            {table: "invoices", selectSQL: `i.rowid,i.*,(SELECT period_index FROM billing_periods p WHERE p.invoice_id=i.id) AS period_index`},
	"payments":            {table: "payment_operations"},
	"immediate-changes":   {table: "immediate_changes"},
	"credits":             {table: "credit_grants"},
	"refunds":             {table: "refund_operations"},
	"outbox":              {table: "outbox", filter: `status='pending'`},
	"meters":              {table: "meter_schemas"},
	"account-migrations":  {table: "account_links"},
	"legacy-provenance":   {table: "legacy_provenance"},
	"reconciliation-runs": {table: "reconciliation_runs"},
	"discrepancies":       {table: "discrepancies"},
}

func adminColumnName(column string) string {
	switch column {
	case "fixed_amount_minor":
		return "FixedMinor"
	case "publication_state":
		return "State"
	}
	parts := strings.Split(column, "_")
	for i, part := range parts {
		if part == "id" {
			parts[i] = "ID"
		} else if part == "utc" {
			parts[i] = "UTC"
		} else if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

func adminResourceValue(column string, value any) any {
	if data, ok := value.([]byte); ok {
		return string(data)
	}
	if number, ok := value.(int64); ok && (strings.HasSuffix(column, "_at") || strings.HasSuffix(column, "_from") || strings.HasSuffix(column, "_until") || strings.HasSuffix(column, "_to") || strings.HasSuffix(column, "_start") || strings.HasSuffix(column, "_end")) {
		return time.Unix(0, number).UTC().Format(time.RFC3339Nano)
	}
	return value
}

func (l *Lab) AdminOverviewCounts(ctx context.Context) (map[string]int, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tables := map[string]string{
		"quotes": "quotes", "subscriptions": "subscriptions", "invoices": "invoices",
		"payments": "payment_operations", "refunds": "refund_operations",
		"credits": "credit_grants", "price_versions": "price_versions",
		"pending_outbox": "outbox WHERE status='pending'",
	}
	counts := make(map[string]int, len(tables))
	for key, table := range tables {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return counts, nil
}

// AdminResourcePage uses an insertion-order cursor so new rows cannot shift a
// page already read. It reads only the requested resource, never the full lab.
func (l *Lab) AdminResourcePage(ctx context.Context, name string, after int64, limit int) ([]map[string]any, int, int64, error) {
	return l.AdminFilteredResourcePage(ctx, name, after, limit, nil)
}

// AdminFilteredResourcePage binds values to server-owned SQL columns and keeps
// the insertion-order cursor semantics of the unfiltered list.
func (l *Lab) AdminFilteredResourcePage(ctx context.Context, name string, after int64, limit int, filters map[string]string) ([]map[string]any, int, int64, error) {
	spec, ok := adminResourceSpecs[name]
	if !ok {
		return nil, 0, 0, ErrAdminUnsupportedAction
	}
	if after < 0 || limit < 1 || limit > 100 {
		return nil, 0, 0, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, 0, err
	}
	defer tx.Rollback()
	filter := ""
	if spec.filter != "" {
		filter = " AND " + spec.filter
	}
	filterArgs := make([]any, 0, len(filters)*2)
	var createdFrom, createdBefore int64
	var hasCreatedFrom, hasCreatedBefore bool
	keys := make([]string, 0, len(filters))
	for key := range filters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		column, allowed := adminResourceFilterColumns[name][key]
		value := filters[key]
		if !allowed || value == "" || len(value) > 256 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, 0, 0, ErrAdminInvalidCommand
		}
		if key == "period_index" {
			index, err := strconv.ParseUint(value, 10, 32)
			if err != nil || strconv.FormatUint(index, 10) != value {
				return nil, 0, 0, ErrAdminInvalidCommand
			}
			filter += " AND " + column + "=?"
			filterArgs = append(filterArgs, index)
			continue
		}
		if key == "created_from" || key == "created_before" {
			if _, err := canonicalAdminUTC(value); err != nil {
				return nil, 0, 0, ErrAdminInvalidCommand
			}
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return nil, 0, 0, ErrAdminInvalidCommand
			}
			nano := parsed.UnixNano()
			if key == "created_from" {
				createdFrom, hasCreatedFrom = nano, true
				filter += " AND " + column + ">=?"
			} else {
				createdBefore, hasCreatedBefore = nano, true
				filter += " AND " + column + "<?"
			}
			filterArgs = append(filterArgs, nano)
			continue
		}
		if key == "id_prefix" {
			filter += " AND substr(" + column + ",1,length(?))=?"
			filterArgs = append(filterArgs, value, value)
		} else {
			filter += " AND " + column + "=?"
			filterArgs = append(filterArgs, value)
		}
	}
	if hasCreatedFrom && hasCreatedBefore && createdFrom >= createdBefore {
		return nil, 0, 0, ErrAdminInvalidCommand
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+spec.table+` WHERE 1=1`+filter, filterArgs...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}
	selection := spec.selectSQL
	if selection == "" {
		selection = "t.rowid,t.*"
	}
	query := fmt.Sprintf(`SELECT %s FROM %s t WHERE t.rowid>?%s ORDER BY t.rowid LIMIT ?`, selection, spec.table, filter)
	// Custom selections use a table alias matching their expression.
	if spec.selectSQL != "" {
		alias := "q"
		if name == "prices" {
			alias = "t"
		}
		if name == "subscriptions" {
			alias = "s"
		} else if name == "invoices" {
			alias = "i"
		}
		query = fmt.Sprintf(`SELECT %s FROM %s %s WHERE %s.rowid>?%s ORDER BY %s.rowid LIMIT ?`, selection, spec.table, alias, alias, filter, alias)
	}
	pageArgs := append([]any{after}, filterArgs...)
	pageArgs = append(pageArgs, limit+1)
	rows, err := tx.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, 0, 0, err
	}
	items := make([]map[string]any, 0, limit)
	last := after
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, 0, 0, err
		}
		if len(items) == limit {
			break
		}
		rowid, ok := values[0].(int64)
		if !ok {
			return nil, 0, 0, sql.ErrNoRows
		}
		last = rowid
		item := make(map[string]any, len(columns)-1)
		for i := 1; i < len(columns); i++ {
			item[adminColumnName(columns[i])] = adminResourceValue(columns[i], values[i])
		}
		if name == "prices" {
			// Bootstrap prices predate publication tracking; their zero is unknown provenance.
			seededBaseline := item["ID"] == "basic-v1" || item["ID"] == "pro-v1"
			if item["State"] == "draft" || (seededBaseline && item["PublishedAt"] == "1970-01-01T00:00:00Z") {
				item["PublishedAt"] = nil
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, 0, err
	}
	if len(items) < limit {
		last = 0
	} else {
		var exists int
		existsArgs := append([]any{last}, filterArgs...)
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+spec.table+` WHERE rowid>?`+filter+`)`, existsArgs...).Scan(&exists); err != nil {
			return nil, 0, 0, err
		}
		if exists == 0 {
			last = 0
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, 0, err
	}
	return items, total, last, nil
}
