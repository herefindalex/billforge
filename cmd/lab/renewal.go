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

func runRenewalDemo() error {
	dir, err := os.MkdirTemp("", "billforge-renewal-")
	if err != nil {
		return err
	}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	local, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := lab.Open(local, provider, func() time.Time { return now })
	if err != nil {
		return err
	}
	defer l.Close()
	ctx := context.Background()
	q, err := l.CreateQuote(ctx, "demo-customer", "basic")
	if err != nil {
		return err
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "renewal-demo:"+q.ID)
	if err != nil {
		return err
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		return err
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		return err
	}
	var steps []step
	add := func(stage string) error {
		s, err := l.Snapshot(ctx, r.SubscriptionID)
		if err != nil {
			return err
		}
		steps = append(steps, step{stage, s})
		return nil
	}
	if err := add("initial_paid"); err != nil {
		return err
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil {
		return err
	}
	if len(renewed) != 1 {
		return errors.New("expected one renewal")
	}
	if err := l.SetFakePaymentDecision(ctx, renewed[0].OperationID, "definitively_failed"); err != nil {
		return err
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		return err
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		return err
	}
	if err := add("renewal_failed_grace"); err != nil {
		return err
	}
	now = now.Add(7 * 24 * time.Hour)
	if err := l.RefreshEntitlements(ctx); err != nil {
		return err
	}
	if err := add("grace_expired"); err != nil {
		return err
	}
	retryID, err := l.RetryFailedPayment(ctx, renewed[0].InvoiceID, "renewal-demo-retry")
	if err != nil {
		return err
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		return err
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		return err
	}
	if err := add("retry_paid"); err != nil {
		return err
	}
	periods, err := l.Periods(ctx, r.SubscriptionID)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		CommerceDB string              `json:"commerce_db"`
		ProviderDB string              `json:"provider_db"`
		Initial    lab.Receipt         `json:"initial"`
		Renewal    lab.Receipt         `json:"renewal"`
		RetryID    string              `json:"retry_operation_id"`
		Periods    []lab.BillingPeriod `json:"periods"`
		Steps      []step              `json:"steps"`
	}{local, provider, r, renewed[0], retryID, periods, steps})
}
