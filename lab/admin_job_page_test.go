package lab

import (
	"context"
	"fmt"
	"testing"
)

func TestAdminJobsPageKeepsCursorStableAndCountsItems(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	insertJob := func(number int, statuses ...string) {
		t.Helper()
		id := fmt.Sprintf("page-job-%d", number)
		commandID := fmt.Sprintf("page-command-%d", number)
		stamp := "2026-09-28T12:00:00Z"
		_, err := l.db.ExecContext(ctx, `INSERT INTO admin_commands(id,actor_id,idempotency_key,action_id,target_id,payload_json,payload_hash,status,business_time,created_at,updated_at)
			VALUES(?, 'local-admin', ?, 'C44', '', '{}', 'test-hash', 'succeeded', ?, ?, ?)`, commandID, commandID, stamp, stamp, stamp)
		if err != nil {
			t.Fatal(err)
		}
		_, err = l.db.ExecContext(ctx, `INSERT INTO admin_jobs(id,command_id,kind,status,created_at,updated_at)
			VALUES(?, ?, 'C44', 'partial', ?, ?)`, id, commandID, stamp, stamp)
		if err != nil {
			t.Fatal(err)
		}
		for i, status := range statuses {
			_, err = l.db.ExecContext(ctx, `INSERT INTO admin_job_items(id,job_id,target_type,target_id,payload_hash,status,updated_at)
				VALUES(?, ?, 'subscription', ?, 'test-hash', ?, ?)`, fmt.Sprintf("%s-item-%d", id, i), id, fmt.Sprintf("subscription-%d", i), status, stamp)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	insertJob(1, "succeeded")
	insertJob(2, "succeeded", "conflicted", "waiting_verification")
	first, cursor, err := l.AdminJobsPage(ctx, 0, 1)
	if err != nil || len(first) != 1 || first[0].ID != "page-job-2" || cursor == 0 {
		t.Fatalf("first page: jobs=%+v cursor=%d err=%v", first, cursor, err)
	}
	if first[0].TotalItems != 3 || first[0].SucceededItems != 1 || first[0].AttentionItems != 2 {
		t.Fatalf("wrong per-job counts: %+v", first[0])
	}
	insertJob(3, "succeeded")
	second, next, err := l.AdminJobsPage(ctx, cursor, 1)
	if err != nil || len(second) != 1 || second[0].ID != "page-job-1" || next != 0 {
		t.Fatalf("cursor shifted by newer job: jobs=%+v cursor=%d err=%v", second, next, err)
	}
	if _, _, err := l.AdminJobsPage(ctx, -1, 1); err != ErrAdminInvalidCommand {
		t.Fatalf("negative cursor accepted: %v", err)
	}
}
