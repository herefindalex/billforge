package lab

import "context"

type InvoiceLine struct {
	PriceVersionID string `json:"price_version_id"`
	ComponentCode  string `json:"component_code"`
	AmountMinor    int64  `json:"amount_minor"`
}

func (l *Lab) InvoiceLines(ctx context.Context, invoiceID string) ([]InvoiceLine, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT price_version_id,component_code,amount_minor FROM invoice_lines WHERE invoice_id=? ORDER BY rowid`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InvoiceLine
	for rows.Next() {
		var line InvoiceLine
		if err := rows.Scan(&line.PriceVersionID, &line.ComponentCode, &line.AmountMinor); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}
