package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminPublishPriceReceiptFailureRecoversWithoutPartialVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := func() time.Time { return fixedNow }
	l, err := Open(commerce, provider, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"id":"receipt-pro-v2","version":"2","fixed_minor":"6000","seat_minor":"1000","included_tasks":"100","usage_rate_num":"1","usage_rate_den":"1","effective_from":"` + fixedNow.Format(time.RFC3339Nano) + `"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C18", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "receipt-price-key", "C18", "", payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit: %+v replay=%t err=%v", command, replay, err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_price_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure should roll back published price")
	}
	var versions, components, receipts int
	readCounts := func() {
		t.Helper()
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_versions WHERE id='receipt-pro-v2'`).Scan(&versions); err != nil {
			t.Fatal(err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_components WHERE price_version_id='receipt-pro-v2'`).Scan(&components); err != nil {
			t.Fatal(err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
	}
	readCounts()
	if versions != 0 || components != 0 || receipts != 0 {
		t.Fatalf("receipt failure left partial price: versions=%d components=%d receipts=%d", versions, components, receipts)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := l.AdminCommand(ctx, command.ID)
	if err != nil || recovered.Status != "succeeded" {
		t.Fatalf("original command not recovered: %+v err=%v", recovered, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "receipt-price-key", "C18", "", payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("idempotent replay changed command: %+v replay=%t err=%v", replayed, replay, err)
	}
	readCounts()
	if versions != 1 || components != 3 || receipts != 1 {
		t.Fatalf("recovery duplicated or lost price: versions=%d components=%d receipts=%d", versions, components, receipts)
	}
}

func TestAdminCatalogDependencyChainReceiptFailuresRecover(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := func() time.Time { return fixedNow }
	l, err := Open(commerce, provider, now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	effective := fixedNow.Format(time.RFC3339Nano)
	cases := []struct {
		action, key string
		payload     json.RawMessage
		countSQL    string
		auxSQL      string
		wantAux     int
	}{
		{"C19", "receipt-meter-key", json.RawMessage(`{"id":"receipt_api_calls","source":"api","unit":"request","schema_version":"1"}`),
			`SELECT COUNT(*) FROM meter_schemas WHERE id='receipt_api_calls'`, "", 0},
		{"C20", "receipt-metered-price-key", json.RawMessage(`{"id":"receipt_ai_v1","plan_id":"receipt_ai","version":"1","fixed_minor":"3000","seat_minor":"0","meter_id":"receipt_api_calls","included_quantity":"100","usage_rate_num":"2","usage_rate_den":"1","effective_from":"` + effective + `"}`),
			`SELECT COUNT(*) FROM price_versions WHERE id='receipt_ai_v1'`, `SELECT COUNT(*) FROM price_components WHERE price_version_id='receipt_ai_v1'`, 2},
		{"C21", "receipt-selection-key", json.RawMessage(`{"plan_id":"receipt_ai","cohort":"default","effective_at":"` + effective + `","price_version_id":"receipt_ai_v1"}`),
			`SELECT COUNT(*) FROM catalog_selection WHERE plan_id='receipt_ai' AND cohort='default'`, "", 0},
	}
	for _, tc := range cases {
		preview, err := l.AdminCreatePreview(ctx, "local-admin", tc.action, "", tc.payload)
		if err != nil {
			t.Fatalf("%s preview: %v", tc.action, err)
		}
		command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, tc.action, "", tc.payload, preview.ID)
		if err != nil || replay {
			t.Fatalf("%s submit: %+v replay=%t err=%v", tc.action, command, replay, err)
		}
		if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_catalog_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
			t.Fatalf("%s receipt failure should roll back domain facts", tc.action)
		}
		var facts, aux, receipts int
		readCounts := func() {
			t.Helper()
			if err := l.db.QueryRowContext(ctx, tc.countSQL).Scan(&facts); err != nil {
				t.Fatal(err)
			}
			if tc.auxSQL != "" {
				if err := l.db.QueryRowContext(ctx, tc.auxSQL).Scan(&aux); err != nil {
					t.Fatal(err)
				}
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
		}
		readCounts()
		if facts != 0 || aux != 0 || receipts != 0 {
			t.Fatalf("%s left partial facts: facts=%d aux=%d receipts=%d", tc.action, facts, aux, receipts)
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		l, err = Open(commerce, provider, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		if err := l.AdminResumeAccepted(ctx); err != nil {
			t.Fatalf("%s resume: %v", tc.action, err)
		}
		recovered, err := l.AdminCommand(ctx, command.ID)
		if err != nil || recovered.Status != "succeeded" {
			t.Fatalf("%s original command not recovered: %+v err=%v", tc.action, recovered, err)
		}
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, tc.action, "", tc.payload, preview.ID)
		if err != nil || !replay || replayed.ID != command.ID {
			t.Fatalf("%s replay changed command: %+v replay=%t err=%v", tc.action, replayed, replay, err)
		}
		readCounts()
		if facts != 1 || aux != tc.wantAux || receipts != 1 {
			t.Fatalf("%s recovery facts=%d aux=%d receipts=%d", tc.action, facts, aux, receipts)
		}
	}
	var selectedPrice, overageMeter string
	if err := l.db.QueryRowContext(ctx, `SELECT price_version_id FROM catalog_selection WHERE plan_id='receipt_ai' AND cohort='default'`).Scan(&selectedPrice); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT meter_id FROM price_components WHERE price_version_id='receipt_ai_v1' AND kind='usage_overage'`).Scan(&overageMeter); err != nil {
		t.Fatal(err)
	}
	if selectedPrice != "receipt_ai_v1" || overageMeter != "receipt_api_calls" {
		t.Fatalf("recovered catalog chain selected=%q overage_meter=%q", selectedPrice, overageMeter)
	}
}

func TestAdminMeterRegistrationRejectsStaleConflictingSchema(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	meterID := "stale_meter_admin"
	requestPayload := json.RawMessage(`{"id":"stale_meter_admin","source":"api","unit":"request","schema_version":"1"}`)
	tokenPayload := json.RawMessage(`{"id":"stale_meter_admin","source":"api","unit":"token","schema_version":"1"}`)
	requestPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C19", "", requestPayload)
	if err != nil {
		t.Fatal(err)
	}
	tokenPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C19", "", tokenPayload)
	if err != nil {
		t.Fatal(err)
	}
	requestCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-meter-request", "C19", "", requestPayload, requestPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-meter-token", "C19", "", tokenPayload, tokenPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenCommand, err = l.AdminExecuteCommand(ctx, tokenCommand.ID)
	if err != nil || tokenCommand.Status != "succeeded" {
		t.Fatalf("winning meter command %+v %v", tokenCommand, err)
	}
	winnerReplay, found, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-meter-token", "C19", "", tokenPayload, tokenPreview.ID)
	if err != nil || !found || winnerReplay.ID != tokenCommand.ID || winnerReplay.Status != "succeeded" {
		t.Fatalf("winning meter command replay %+v found=%v err=%v", winnerReplay, found, err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-meter-token", "C19", "", requestPayload, tokenPreview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed payload reused winning meter key: %v", err)
	}
	requestCommand, err = l.AdminExecuteCommand(ctx, requestCommand.ID)
	if err != nil || requestCommand.Status != "failed" || requestCommand.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale meter command %+v %v", requestCommand, err)
	}
	var unit string
	if err := l.db.QueryRowContext(ctx, `SELECT unit FROM meter_schemas WHERE id=?`, meterID).Scan(&unit); err != nil || unit != "token" {
		t.Fatalf("registered meter unit %q %v", unit, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, requestCommand.ID, tokenCommand.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("meter command receipts %d %v", receipts, err)
	}
	replayed, found, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-meter-request", "C19", "", requestPayload, requestPreview.ID)
	if err != nil || !found || replayed.ID != requestCommand.ID || replayed.Status != "failed" {
		t.Fatalf("stale meter command replay %+v found=%v err=%v", replayed, found, err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C19", "", requestPayload); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting schema should no longer preview: %v", err)
	}
}

func TestAdminMeteredPriceRejectsStaleConflictingVersion(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.RegisterMeter(ctx, MeterSpec{ID: "metered_conflict_meter", Source: "api", Unit: "token", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	base := adminMeteredPricePayload{
		ID: "metered_conflict_v1", PlanID: "metered_conflict", Version: "1",
		FixedMinor: "3000", SeatMinor: "0", MeterID: "metered_conflict_meter",
		IncludedQuantity: "100", UsageRateNum: "2", UsageRateDen: "1",
		EffectiveFrom: fixedNow.Format(time.RFC3339Nano),
	}
	mustJSON := func(p adminMeteredPricePayload) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	original := mustJSON(base)
	base.FixedMinor = "3500"
	updated := mustJSON(base)
	originalPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C20", "", original)
	if err != nil {
		t.Fatal(err)
	}
	updatedPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C20", "", updated)
	if err != nil {
		t.Fatal(err)
	}
	originalCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "metered-price-original", "C20", "", original, originalPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "metered-price-updated", "C20", "", updated, updatedPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedCommand, err = l.AdminExecuteCommand(ctx, updatedCommand.ID)
	if err != nil || updatedCommand.Status != "succeeded" {
		t.Fatalf("winning metered price %+v %v", updatedCommand, err)
	}
	originalCommand, err = l.AdminExecuteCommand(ctx, originalCommand.ID)
	if err != nil || originalCommand.Status != "failed" || originalCommand.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale metered price %+v %v", originalCommand, err)
	}
	var amount, components, receipts int
	var checksum string
	if err := l.db.QueryRowContext(ctx, `SELECT fixed_amount_minor,checksum FROM price_versions WHERE id=?`, base.ID).Scan(&amount, &checksum); err != nil || amount != 3500 || checksum == "" {
		t.Fatalf("published price amount=%d checksum=%q err=%v", amount, checksum, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_components WHERE price_version_id=?`, base.ID).Scan(&components); err != nil || components != 2 {
		t.Fatalf("price components %d %v", components, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, originalCommand.ID, updatedCommand.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("metered price receipts %d %v", receipts, err)
	}
	for _, replay := range []struct {
		key, previewID string
		payload        json.RawMessage
		want           AdminCommand
	}{
		{key: "metered-price-original", previewID: originalPreview.ID, payload: original, want: originalCommand},
		{key: "metered-price-updated", previewID: updatedPreview.ID, payload: updated, want: updatedCommand},
	} {
		got, found, err := l.AdminSubmitCommand(ctx, "local-admin", replay.key, "C20", "", replay.payload, replay.previewID)
		if err != nil || !found || got.ID != replay.want.ID || got.Status != replay.want.Status {
			t.Fatalf("metered price replay %+v found=%v err=%v", got, found, err)
		}
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "metered-price-updated", "C20", "", original, updatedPreview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed metered price reused key: %v", err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C20", "", original); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting published price should not preview: %v", err)
	}
	base.PlanID = "metered_occupied_plan"
	base.ID = "metered_occupied_a"
	firstPayload := mustJSON(base)
	base.ID = "metered_occupied_b"
	secondPayload := mustJSON(base)
	firstPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C20", "", firstPayload)
	if err != nil {
		t.Fatal(err)
	}
	secondPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C20", "", secondPayload)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := l.AdminSubmitCommand(ctx, "local-admin", "metered-occupied-first", "C20", "", firstPayload, firstPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := l.AdminSubmitCommand(ctx, "local-admin", "metered-occupied-second", "C20", "", secondPayload, secondPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err = l.AdminExecuteCommand(ctx, first.ID)
	if err != nil || first.Status != "succeeded" {
		t.Fatalf("occupied version winner %+v %v", first, err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "failed" || second.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("occupied version loser %+v %v", second, err)
	}
	var versions int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_versions WHERE plan_id=? AND version=1`, base.PlanID).Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("occupied plan/version rows %d %v", versions, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, first.ID, second.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("occupied version receipts %d %v", receipts, err)
	}
}

func TestAdminCatalogSelectionRejectsStaleCompetingPrice(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []ProPriceSpec{
		{ID: "selection_conflict_v2", Version: 2, FixedMinor: 6000, SeatMinor: 1000, IncludedTasks: 100, UsageRateNum: 1, UsageRateDen: 1, EffectiveFrom: fixedNow},
		{ID: "selection_conflict_v3", Version: 3, FixedMinor: 7000, SeatMinor: 1000, IncludedTasks: 100, UsageRateNum: 1, UsageRateDen: 1, EffectiveFrom: fixedNow},
	} {
		if _, err := l.PublishProPrice(ctx, spec); err != nil {
			t.Fatal(err)
		}
	}
	payload := func(priceID string) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(adminSelectionPayload{
			PlanID: "pro", Cohort: "selection_conflict", EffectiveAt: fixedNow.Format(time.RFC3339Nano), PriceVersionID: priceID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	v2Payload, v3Payload := payload("selection_conflict_v2"), payload("selection_conflict_v3")
	v2Preview, err := l.AdminCreatePreview(ctx, "local-admin", "C21", "", v2Payload)
	if err != nil {
		t.Fatal(err)
	}
	v3Preview, err := l.AdminCreatePreview(ctx, "local-admin", "C21", "", v3Payload)
	if err != nil {
		t.Fatal(err)
	}
	v2Command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "selection-conflict-v2", "C21", "", v2Payload, v2Preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	v3Command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "selection-conflict-v3", "C21", "", v3Payload, v3Preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	v3Command, err = l.AdminExecuteCommand(ctx, v3Command.ID)
	if err != nil || v3Command.Status != "succeeded" {
		t.Fatalf("winning selection %+v %v", v3Command, err)
	}
	v2Command, err = l.AdminExecuteCommand(ctx, v2Command.ID)
	if err != nil || v2Command.Status != "failed" || v2Command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale selection %+v %v", v2Command, err)
	}
	var selected string
	if err := l.db.QueryRowContext(ctx, `SELECT price_version_id FROM catalog_selection WHERE plan_id='pro' AND cohort='selection_conflict' AND effective_at=?`, fixedNow.UnixNano()).Scan(&selected); err != nil || selected != "selection_conflict_v3" {
		t.Fatalf("selected price %q %v", selected, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, v2Command.ID, v3Command.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("selection receipts %d %v", receipts, err)
	}
	for _, replay := range []struct {
		key, previewID string
		payload        json.RawMessage
		want           AdminCommand
	}{
		{key: "selection-conflict-v2", previewID: v2Preview.ID, payload: v2Payload, want: v2Command},
		{key: "selection-conflict-v3", previewID: v3Preview.ID, payload: v3Payload, want: v3Command},
	} {
		got, found, err := l.AdminSubmitCommand(ctx, "local-admin", replay.key, "C21", "", replay.payload, replay.previewID)
		if err != nil || !found || got.ID != replay.want.ID || got.Status != replay.want.Status {
			t.Fatalf("selection replay %+v found=%v err=%v", got, found, err)
		}
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "selection-conflict-v3", "C21", "", v2Payload, v3Preview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed selection reused key: %v", err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C21", "", v2Payload); !errors.Is(err, ErrConflict) {
		t.Fatalf("occupied selection should not preview: %v", err)
	}
}

func TestAdminCatalogPublishAndSelectAtomicReceipts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	effective := time.Now().UTC().Truncate(time.Second).Add(-time.Minute).Format(time.RFC3339Nano)
	cases := []struct {
		action, key string
		payload     json.RawMessage
	}{
		{"C18", "publish-pro-admin-001", json.RawMessage(`{"id":"pro-admin-v2","version":"2","fixed_minor":"6000","seat_minor":"1000","included_tasks":"100","usage_rate_num":"1","usage_rate_den":"1","effective_from":"` + effective + `"}`)},
		{"C19", "register-meter-admin-001", json.RawMessage(`{"id":"api_calls","source":"api","unit":"request","schema_version":"1"}`)},
		{"C20", "publish-metered-admin-001", json.RawMessage(`{"id":"ai_admin_v1","plan_id":"ai_admin","version":"1","fixed_minor":"3000","seat_minor":"0","meter_id":"api_calls","included_quantity":"100","usage_rate_num":"2","usage_rate_den":"1","effective_from":"` + effective + `"}`)},
		{"C21", "select-catalog-admin-001", json.RawMessage(`{"plan_id":"ai_admin","cohort":"default","effective_at":"` + effective + `","price_version_id":"ai_admin_v1"}`)},
	}
	for _, tc := range cases {
		preview, err := l.AdminCreatePreview(ctx, "local-admin", tc.action, "", tc.payload)
		if err != nil {
			t.Fatalf("%s preview: %v", tc.action, err)
		}
		if tc.action == "C18" || tc.action == "C20" {
			var impact map[string]string
			if err := json.Unmarshal(preview.Impact, &impact); err != nil {
				t.Fatal(err)
			}
			if impact["currency"] != "USD" {
				t.Fatalf("%s preview currency = %q", tc.action, impact["currency"])
			}
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, tc.action, "", tc.payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", tc.action, err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v %v", tc.action, command, err)
		}
	}
	var prices, meters, selections, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_versions WHERE id IN ('pro-admin-v2','ai_admin_v1') AND publication_state='published'`).Scan(&prices); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_schemas WHERE id='api_calls'`).Scan(&meters); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_selection WHERE plan_id='ai_admin' AND cohort='default' AND price_version_id='ai_admin_v1'`).Scan(&selections); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if prices != 2 || meters != 1 || selections != 1 || receipts != 4 {
		t.Fatalf("prices=%d meters=%d selections=%d receipts=%d", prices, meters, selections, receipts)
	}
}
