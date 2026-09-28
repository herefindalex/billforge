package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"billforge/lab"
)

type step struct {
	Stage    string       `json:"stage"`
	Snapshot lab.Snapshot `json:"snapshot"`
}

func run() error {
	if len(os.Args) >= 2 && os.Args[1] == "admin" {
		return runAdmin(os.Args[2:])
	}
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		return runServe(os.Args[2:])
	}
	if len(os.Args) == 1 || os.Args[1] == "menu" {
		if len(os.Args) != 1 && len(os.Args) != 2 && len(os.Args) != 4 {
			return errors.New("usage: lab menu [commerce.db provider.db]")
		}
		var paths []string
		if len(os.Args) == 4 {
			paths = os.Args[2:4]
		}
		return runMenu(os.Stdin, os.Stdout, paths)
	}
	if len(os.Args) >= 2 && os.Args[1] == "correction-demo" {
		if len(os.Args) != 2 {
			return errors.New("usage: lab correction-demo")
		}
		return runCorrectionDemo()
	}
	if len(os.Args) >= 2 && os.Args[1] == "renewal-demo" {
		if len(os.Args) != 2 {
			return errors.New("usage: lab renewal-demo")
		}
		return runRenewalDemo()
	}
	if len(os.Args) < 2 || os.Args[1] != "demo" || len(os.Args) == 3 || len(os.Args) > 5 {
		return errors.New("usage: lab demo [commerce.db provider.db [normal|lost_response|crash_after_provider]] | lab renewal-demo | lab correction-demo")
	}
	var local, provider, fault string
	if len(os.Args) >= 4 {
		local, provider = os.Args[2], os.Args[3]
	} else {
		dir, err := os.MkdirTemp("", "billforge-demo-")
		if err != nil {
			return err
		}
		local, provider = filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	}
	if len(os.Args) == 5 && os.Args[4] != "normal" {
		fault = os.Args[4]
	}
	if fault != "" && fault != "lost_response" && fault != "crash_after_provider" {
		return errors.New("unknown fault mode")
	}
	ctx := context.Background()
	l, err := lab.Open(local, provider, nil)
	if err != nil {
		return err
	}
	defer func() { l.Close() }()
	q, err := l.CreateQuote(ctx, "demo-customer", "basic")
	if err != nil {
		return err
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "demo:"+q.ID)
	if err != nil {
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
	if err := add("accepted"); err != nil {
		return err
	}
	_, err = l.DispatchNext(ctx, fault)
	if err != nil && !errors.Is(err, lab.ErrPaymentUnknown) && !errors.Is(err, lab.ErrInjectedCrash) {
		return err
	}
	if err := add("after_provider"); err != nil {
		return err
	}
	if fault != "" {
		// A new process sees both durable files and queries the original key.
		if err := l.Close(); err != nil {
			return err
		}
		l, err = lab.Open(local, provider, nil)
		if err != nil {
			return err
		}
		found, err := l.ReconcilePayment(ctx, r.OperationID)
		if err != nil {
			return fmt.Errorf("provider lookup: %w", err)
		}
		if !found {
			return errors.New("provider lookup has no final result")
		}
		if err := add("reconciled"); err != nil {
			return err
		}
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		return err
	}
	if err := add("entitlement_rebuilt"); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		CommerceDB string      `json:"commerce_db"`
		ProviderDB string      `json:"provider_db"`
		Quote      lab.Quote   `json:"quote"`
		Receipt    lab.Receipt `json:"receipt"`
		Steps      []step      `json:"steps"`
	}{local, provider, q, r, steps})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
