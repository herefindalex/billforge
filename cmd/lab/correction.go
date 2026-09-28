package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"billforge/lab"
)

func runCorrectionDemo() error {
	dir, err := os.MkdirTemp("", "billforge-correction-")
	if err != nil {
		return err
	}
	local := filepath.Join(dir, "commerce.db")
	provider := filepath.Join(dir, "provider.db")
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l, err := lab.Open(local, provider, func() time.Time { return now })
	if err != nil {
		return err
	}
	defer l.Close()
	ctx := context.Background()
	quote, err := l.CreateQuote(ctx, "demo-customer", "basic")
	if err != nil {
		return err
	}
	initial, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "correction-demo:purchase")
	if err != nil {
		return err
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		return err
	}
	correction, err := l.PostReduction(ctx, initial.InvoiceID, 400, "demo service credit", "correction-demo:reduce")
	if err != nil {
		return err
	}
	if len(correction.GrantIDs) != 1 {
		return errors.New("expected one funded credit grant")
	}
	grantID := correction.GrantIDs[0]
	initialBalance, err := l.Balance(ctx, initial.InvoiceID)
	if err != nil {
		return err
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil {
		return err
	}
	if len(renewals) != 1 {
		return errors.New("expected one renewal")
	}
	renewal := renewals[0]
	if _, err := l.ApplyCredit(ctx, grantID, renewal.InvoiceID, 200, "correction-demo:apply"); err != nil {
		return err
	}
	if _, err := l.CreatePayment(ctx, renewal.InvoiceID, 1800, "correction-demo:renewal-payment"); err != nil {
		return err
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		return err
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		return err
	}
	renewalBalance, err := l.Balance(ctx, renewal.InvoiceID)
	if err != nil {
		return err
	}
	refundID, err := l.ReserveRefund(ctx, grantID, 200, "correction-demo:refund")
	if err != nil {
		return err
	}
	if _, err := l.DispatchRefundNext(ctx, ""); err != nil {
		return err
	}
	refund, err := l.Refund(ctx, refundID)
	if err != nil {
		return err
	}
	credit, err := l.CreditBalance(ctx, grantID)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		CommerceDB     string               `json:"commerce_db"`
		ProviderDB     string               `json:"provider_db"`
		Initial        lab.Receipt          `json:"initial"`
		Correction     lab.CorrectionResult `json:"correction"`
		InitialBalance lab.InvoiceBalance   `json:"initial_balance"`
		Renewal        lab.Receipt          `json:"renewal"`
		RenewalBalance lab.InvoiceBalance   `json:"renewal_balance"`
		Refund         lab.RefundInfo       `json:"refund"`
		Credit         lab.CreditBalance    `json:"credit"`
	}{local, provider, initial, correction, initialBalance, renewal, renewalBalance, refund, credit})
}
