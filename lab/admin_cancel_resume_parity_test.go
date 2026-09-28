package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCancelResumeAndBoundaryMatchesDomain(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt := paidBasicSubscription(t, domain, "cancel-parity")
	adminReceipt := paidBasicSubscription(t, admin, "cancel-parity")
	domainSub, adminSub := domainReceipt.SubscriptionID, adminReceipt.SubscriptionID
	for _, l := range []*Lab{domain, admin} {
		if err := l.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	runAdmin := func(action, key, revision string) AdminCommand {
		t.Helper()
		payload, err := json.Marshal(AdminRevisionPayload{Revision: revision})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, adminSub, payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, adminSub, payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	runBatch := func(action, key string) {
		t.Helper()
		payload := json.RawMessage(`{}`)
		preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, "", payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, "", payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
	}
	compare := func(label string, expectedRevision int64, expectedSub, expectedSchedule, expectedEntitlement string, expectedPeriods int) {
		t.Helper()
		read := func(l *Lab, subID string) string {
			t.Helper()
			var status, schedule, entitlement string
			var revision int64
			var periods, assignmentEnds int
			state, err := l.State(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, sub := range state.Subscriptions {
				if sub.ID == subID {
					status, revision, entitlement = sub.Status, sub.Revision, sub.EntitlementStatus
					break
				}
			}
			if status == "" {
				t.Fatalf("subscription %s missing from state", subID)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT group_concat(status, ',') FROM (SELECT status FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' ORDER BY status)`, subID).Scan(&schedule); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, subID).Scan(&periods); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NOT NULL`, subID).Scan(&assignmentEnds); err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%d/%s/%s/%s/%d/%d", revision, status, schedule, entitlement, periods, assignmentEnds)
		}
		expectedEnds := 0
		if expectedSub == "ended" {
			expectedEnds = 1
		}
		want := fmt.Sprintf("%d/%s/%s/%s/%d/%d", expectedRevision, expectedSub, expectedSchedule, expectedEntitlement, expectedPeriods, expectedEnds)
		if d, a := read(domain, domainSub), read(admin, adminSub); d != want || a != want {
			t.Fatalf("%s differs: domain=%s admin=%s want=%s", label, d, a, want)
		}
	}

	if result, err := domain.ScheduleCancel(ctx, domainSub, 1, "cancel-parity-first"); err != nil || result.Revision != 2 {
		t.Fatalf("domain first cancel: %+v err=%v", result, err)
	}
	runAdmin("C05", "cancel-parity-admin-first", "1")
	compare("first scheduled cancel", 2, "active", "scheduled", "active", 1)
	if result, err := domain.ResumeCancel(ctx, domainSub, 2, "cancel-parity-resume"); err != nil || result.Revision != 3 {
		t.Fatalf("domain resume: %+v err=%v", result, err)
	}
	runAdmin("C06", "cancel-parity-admin-resume", "2")
	compare("resumed cancel", 3, "active", "cancelled", "active", 1)
	if result, err := domain.ScheduleCancel(ctx, domainSub, 3, "cancel-parity-second"); err != nil || result.Revision != 4 {
		t.Fatalf("domain second cancel: %+v err=%v", result, err)
	}
	runAdmin("C05", "cancel-parity-admin-second", "3")
	compare("second scheduled cancel", 4, "active", "cancelled,scheduled", "active", 1)

	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewals, err := domain.RunRenewals(ctx); err != nil || len(renewals) != 0 {
		t.Fatalf("domain boundary created renewal: %+v err=%v", renewals, err)
	}
	runBatch("C44", "cancel-parity-boundary")
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runBatch("C45", "cancel-parity-expire")
	compare("cancel boundary", 5, "ended", "applied,cancelled", "expired", 1)
	if renewals, err := domain.RunRenewals(ctx); err != nil || len(renewals) != 0 {
		t.Fatalf("domain ended subscription renewed: %+v err=%v", renewals, err)
	}
	runBatch("C44", "cancel-parity-boundary-replay")
	compare("ended subscription stays ended", 5, "ended", "applied,cancelled", "expired", 1)
}
