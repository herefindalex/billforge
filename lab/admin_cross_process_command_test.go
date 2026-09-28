package lab

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func executeAdminCommandsAcrossProcesses(t *testing.T, ctx context.Context, commercePath, providerPath string, businessTime time.Time, commandIDs ...string) {
	t.Helper()
	dir := t.TempDir()
	gatePath := filepath.Join(dir, "start")
	type worker struct {
		cmd       *exec.Cmd
		output    bytes.Buffer
		readyPath string
	}
	workers := make([]*worker, 0, len(commandIDs))
	for i, commandID := range commandIDs {
		worker := &worker{readyPath: filepath.Join(dir, "ready-"+string(rune('a'+i)))}
		worker.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdminCrossProcessPaymentWorker$")
		worker.cmd.Env = append(os.Environ(),
			"BILLFORGE_TEST_ADMIN_WORKER=1",
			"BILLFORGE_TEST_COMMERCE_PATH="+commercePath,
			"BILLFORGE_TEST_PROVIDER_PATH="+providerPath,
			"BILLFORGE_TEST_COMMAND_ID="+commandID,
			"BILLFORGE_TEST_READY_PATH="+worker.readyPath,
			"BILLFORGE_TEST_GATE_PATH="+gatePath,
			"BILLFORGE_TEST_BUSINESS_TIME="+businessTime.Format(time.RFC3339Nano),
		)
		worker.cmd.Stdout = &worker.output
		worker.cmd.Stderr = &worker.output
		if err := worker.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		workers = append(workers, worker)
	}
	for {
		allReady := true
		for _, worker := range workers {
			if _, err := os.Stat(worker.readyPath); os.IsNotExist(err) {
				allReady = false
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if allReady {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("workers did not become ready: %v", ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(gatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, worker := range workers {
		if err := worker.cmd.Wait(); err != nil {
			t.Fatalf("admin command worker failed: %v\n%s", err, worker.output.String())
		}
	}
}
