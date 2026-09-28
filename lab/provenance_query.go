package lab

import "context"

func (l *Lab) LegacyProvenanceRecords(ctx context.Context) ([]LegacyProvenance, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence FROM legacy_provenance ORDER BY legacy_account_id,legacy_invoice_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]LegacyProvenance, 0)
	for rows.Next() {
		var item LegacyProvenance
		if err := rows.Scan(&item.LegacyInvoiceID, &item.LegacySubscriptionID, &item.LegacyAccountID, &item.CommerceSubscriptionID, &item.CommerceInvoiceID, &item.PriceVersionID, &item.Status, &item.Evidence); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
