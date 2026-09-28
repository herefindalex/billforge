package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"billforge/lab"
)

func TestMenuCompletesCorrectionCreditRefundAndRenewal(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")}
	input := strings.Join([]string{
		"7", "2026-09-01T00:00:00Z",
		"2", "1", "alice", "2", "1", "purchase-1", "0",
		"3", "3", "", "0",
		"5", "1", "1", "400", "service correction", "correction-1", "0",
		"6", "1", "1", "200", "refund-1", "2", "", "0",
		"7", "2026-10-01T00:00:00Z",
		"4", "1", "0",
		"5", "2", "1", "1", "200", "application-1", "0",
		"3", "1", "1", "1800", "renewal-payment-1", "3", "", "0",
		"4", "2", "0",
		"1", "1", "0", "0",
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := runMenu(strings.NewReader(input), &out, paths); err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"已開啟 Commerce", "已保留退款", "Credit 應用 ID", "新增續約發票：1 筆", "Credit ID"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("menu omitted %q in output:\n%s", phrase, out.String())
		}
	}
	if strings.Contains(out.String(), "操作未完成") {
		t.Fatalf("menu flow failed:\n%s", out.String())
	}
	l, err := lab.Open(paths[0], paths[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, err := l.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Subscriptions) != 1 || len(s.Invoices) != 2 || len(s.Credits) != 1 || len(s.Refunds) != 1 {
		t.Fatalf("unexpected persisted state: subscriptions=%d invoices=%d credits=%d refunds=%d", len(s.Subscriptions), len(s.Invoices), len(s.Credits), len(s.Refunds))
	}
	if s.Subscriptions[0].EntitlementStatus != "active" || s.Credits[0].Balance.AvailableMinor != 0 || s.Credits[0].Balance.AppliedMinor != 200 || s.Credits[0].Balance.RefundedMinor != 200 || s.Refunds[0].Status != "succeeded" {
		t.Fatalf("incorrect final state: subscription=%+v credit=%+v refund=%+v", s.Subscriptions[0], s.Credits[0], s.Refunds[0])
	}
	var second bytes.Buffer
	if err := runMenu(strings.NewReader("1\n1\n0\n0\n"), &second, paths); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), s.Invoices[1].ID) {
		t.Fatal("reopened menu did not display persisted renewal invoice")
	}
}

func TestMenuShowsUnknownAndReconcilesOriginalPayment(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")}
	input := strings.Join([]string{
		"7", "2026-09-01T00:00:00Z",
		"2", "1", "alice", "2", "1", "purchase-unknown", "0",
		"3", "3", "lost_response", "0",
		"1", "6", "0",
		"3", "4", "1", "0",
		"0",
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := runMenu(strings.NewReader(input), &out, paths); err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"結果尚未在 Commerce 確認", "Fake provider 使用獨立 SQLite", "Provider 找到原操作結果：true"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("menu omitted %q in output:\n%s", phrase, out.String())
		}
	}
	l, err := lab.Open(paths[0], paths[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, err := l.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := l.ProviderState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Payments) != 1 || s.Payments[0].Status != "succeeded" || len(provider.Captures) != 1 {
		t.Fatalf("reconciliation did not converge: local=%+v provider=%+v", s.Payments, provider.Captures)
	}
}

func TestMenuPromptsForDatabasePaths(t *testing.T) {
	dir := t.TempDir()
	local, provider := filepath.Join(dir, "nested", "commerce.db"), filepath.Join(dir, "nested", "provider.db")
	var out bytes.Buffer
	if err := runMenu(strings.NewReader(local+"\n"+provider+"\n0\n"), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "已開啟 Commerce") {
		t.Fatalf("menu did not open requested paths: %s", out.String())
	}
	l, err := lab.Open(local, provider, nil)
	if err != nil {
		t.Fatalf("prompted database paths were not usable: %v", err)
	}
	defer l.Close()
}

func TestMenuSchedulesProAtNextBoundary(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")}
	input := strings.Join([]string{
		"7", "2026-09-01T00:00:00Z",
		"2", "1", "alice", "2", "1", "purchase-for-change", "0",
		"3", "3", "", "0",
		"8", "1", "1", "pro", "5", "next-pro", "0",
		"7", "2026-10-01T00:00:00Z",
		"4", "1", "0",
		"1", "1", "0", "0",
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := runMenu(strings.NewReader(input), &out, paths); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "操作未完成") || !strings.Contains(out.String(), "pro-v1") {
		t.Fatalf("menu did not apply Pro schedule:\n%s", out.String())
	}
	l, err := lab.Open(paths[0], paths[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s, err := l.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Invoices) != 2 || s.Invoices[1].Balance.OriginalMinor != 10000 || s.Subscriptions[0].PriceVersionID != "pro-v1" {
		t.Fatalf("menu boundary state: %+v", s)
	}
}
