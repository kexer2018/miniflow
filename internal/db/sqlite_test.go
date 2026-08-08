package db

import (
	"context"
	"testing"
	"time"

	"github.com/kexer2018/miniflow/internal/pipeline"
)

// ─── helpers ────────────────────────────────────────────────

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore(:memory:) failed: %v", err)
	}
	return store
}

func testPipelineResult(id string) *pipeline.PipelineResult {
	return &pipeline.PipelineResult{
		PipelineID: id,
		Name:       "test-pipeline-" + id,
		Status:     pipeline.StatusSuccess,
		TotalSteps: 3,
		StepResults: []pipeline.StepResult{
			{Name: "build", Status: pipeline.StatusSuccess, ExitCode: 0, DurationMs: 100},
			{Name: "test", Status: pipeline.StatusSuccess, ExitCode: 0, DurationMs: 200},
			{Name: "deploy", Status: pipeline.StatusSuccess, ExitCode: 0, DurationMs: 300},
		},
		StartedAt:  time.Now().Add(-time.Minute),
		FinishedAt: time.Now(),
		DurationMs: 600,
	}
}

func testExecContext(pipelineID string) *pipeline.ExecContext {
	return &pipeline.ExecContext{
		PipelineID:   pipelineID,
		WorkspaceDir: "/tmp/miniflow/workspaces/" + pipelineID,
		CacheDir:     "/tmp/miniflow/cache/" + pipelineID,
	}
}

func testDiagnosisRecord(pipelineID, stepName string) *DiagnosisRecord {
	return &DiagnosisRecord{
		PipelineID:           pipelineID,
		StepName:             stepName,
		ClassificationType:   "infra_error",
		ClassificationReason: "image pull failure",
		RootCause:            "Image tag not found in registry",
		FixPlan:              "Fix the image tag",
		Confidence:           0.85,
		Category:             "image_pull",
		DiagnosisJSON:        `{"root_cause":"test"}`,
	}
}

// ─── Store Creation ─────────────────────────────────────────

func TestNewSQLiteStore_InMemory(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer store.Close()

	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestNewSQLiteStore_InvalidPath(t *testing.T) {
	_, err := NewSQLiteStore("/nonexistent/dir/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// ─── Pipeline Result CRUD ───────────────────────────────────

func TestPipelineResult_SaveAndGet(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	expected := testPipelineResult("pipe-001")
	if err := store.SavePipelineResult(ctx, expected); err != nil {
		t.Fatalf("SavePipelineResult failed: %v", err)
	}

	got, err := store.GetPipelineResult(ctx, "pipe-001")
	if err != nil {
		t.Fatalf("GetPipelineResult failed: %v", err)
	}

	if got.PipelineID != expected.PipelineID {
		t.Errorf("PipelineID: expected %q, got %q", expected.PipelineID, got.PipelineID)
	}
	if got.Name != expected.Name {
		t.Errorf("Name: expected %q, got %q", expected.Name, got.Name)
	}
	if got.Status != expected.Status {
		t.Errorf("Status: expected %q, got %q", expected.Status, got.Status)
	}
	if got.TotalSteps != expected.TotalSteps {
		t.Errorf("TotalSteps: expected %d, got %d", expected.TotalSteps, got.TotalSteps)
	}
	if got.DurationMs != expected.DurationMs {
		t.Errorf("DurationMs: expected %d, got %d", expected.DurationMs, got.DurationMs)
	}
	if len(got.StepResults) != len(expected.StepResults) {
		t.Fatalf("StepResults count: expected %d, got %d", len(expected.StepResults), len(got.StepResults))
	}
	for i := range expected.StepResults {
		if got.StepResults[i].Name != expected.StepResults[i].Name {
			t.Errorf("StepResult[%d].Name: expected %q, got %q", i, expected.StepResults[i].Name, got.StepResults[i].Name)
		}
	}
}

func TestPipelineResult_DoesNotPersistRawLog(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	result := testPipelineResult("redacted-log")
	result.StepResults[0].RawLog = "token=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB"

	if err := store.SavePipelineResult(context.Background(), result); err != nil {
		t.Fatalf("SavePipelineResult failed: %v", err)
	}
	got, err := store.GetPipelineResult(context.Background(), result.PipelineID)
	if err != nil {
		t.Fatalf("GetPipelineResult failed: %v", err)
	}
	if got.StepResults[0].RawLog != "" {
		t.Fatalf("raw log must not be persisted, got %q", got.StepResults[0].RawLog)
	}
	if got.StepResults[0].Sanitized == result.StepResults[0].RawLog || got.StepResults[0].Sanitized == "" {
		t.Fatalf("expected a non-empty redacted log, got %q", got.StepResults[0].Sanitized)
	}
}

func TestPipelineResult_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetPipelineResult(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent pipeline result")
	}
}

func TestPipelineResult_List(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Save 5 results
	for i := 0; i < 5; i++ {
		id := "pipe-list-" + string(rune('A'+i))
		if err := store.SavePipelineResult(ctx, testPipelineResult(id)); err != nil {
			t.Fatalf("SavePipelineResult %s failed: %v", id, err)
		}
	}

	// List with limit=3
	results, err := store.ListPipelineResults(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListPipelineResults failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestPipelineResult_ListDefaults(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	results, err := store.ListPipelineResults(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListPipelineResults failed: %v", err)
	}
	// With limit=0, should return default (20) but empty list (no data)
	if results == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestPipelineResult_ListOrdering(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	ids := []string{"first", "second", "third"}
	for _, id := range ids {
		r := testPipelineResult(id)
		time.Sleep(time.Second) // created_at has second precision
		if err := store.SavePipelineResult(ctx, r); err != nil {
			t.Fatalf("SavePipelineResult %s failed: %v", id, err)
		}
	}

	results, err := store.ListPipelineResults(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListPipelineResults failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Should be ordered by created_at DESC (most recent first)
	if results[0].PipelineID != "third" {
		t.Errorf("expected first result 'third', got %q", results[0].PipelineID)
	}
	if results[2].PipelineID != "first" {
		t.Errorf("expected last result 'first', got %q", results[2].PipelineID)
	}
}

// ─── ExecContext CRUD ───────────────────────────────────────

func TestExecContext_SaveAndGet(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	expected := testExecContext("ec-001")
	if err := store.SaveExecContext(ctx, expected); err != nil {
		t.Fatalf("SaveExecContext failed: %v", err)
	}

	got, err := store.GetExecContext(ctx, "ec-001")
	if err != nil {
		t.Fatalf("GetExecContext failed: %v", err)
	}

	if got.PipelineID != expected.PipelineID {
		t.Errorf("PipelineID: expected %q, got %q", expected.PipelineID, got.PipelineID)
	}
	if got.WorkspaceDir != expected.WorkspaceDir {
		t.Errorf("WorkspaceDir: expected %q, got %q", expected.WorkspaceDir, got.WorkspaceDir)
	}
	if got.CacheDir != expected.CacheDir {
		t.Errorf("CacheDir: expected %q, got %q", expected.CacheDir, got.CacheDir)
	}
}

func TestExecContext_NotFound(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetExecContext(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent exec context")
	}
}

func TestExecContext_Replace(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	first := testExecContext("ec-replace")
	if err := store.SaveExecContext(ctx, first); err != nil {
		t.Fatalf("SaveExecContext failed: %v", err)
	}

	second := &pipeline.ExecContext{
		PipelineID:   "ec-replace",
		WorkspaceDir: "/updated/workspace",
		CacheDir:     "/updated/cache",
	}
	if err := store.SaveExecContext(ctx, second); err != nil {
		t.Fatalf("SaveExecContext (replace) failed: %v", err)
	}

	got, err := store.GetExecContext(ctx, "ec-replace")
	if err != nil {
		t.Fatalf("GetExecContext failed: %v", err)
	}
	if got.WorkspaceDir != "/updated/workspace" {
		t.Errorf("expected updated workspace, got %q", got.WorkspaceDir)
	}
}

// ─── Diagnosis History CRUD ─────────────────────────────────

func TestDiagnosis_SaveAndList(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	records := []*DiagnosisRecord{
		testDiagnosisRecord("pipe-d-001", "build"),
		testDiagnosisRecord("pipe-d-001", "test"),
		testDiagnosisRecord("pipe-d-002", "deploy"),
	}
	for _, r := range records {
		if err := store.SaveDiagnosis(ctx, r); err != nil {
			t.Fatalf("SaveDiagnosis failed: %v", err)
		}
	}

	listed, err := store.ListDiagnoses(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListDiagnoses failed: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 diagnoses, got %d", len(listed))
	}

	// Verify fields
	for _, r := range listed {
		if r.PipelineID == "" {
			t.Error("PipelineID should not be empty")
		}
		if r.ClassificationType == "" {
			t.Error("ClassificationType should not be empty")
		}
		if r.RootCause == "" {
			t.Error("RootCause should not be empty")
		}
	}
}

func TestDiagnosis_ListEmpty(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	records, err := store.ListDiagnoses(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListDiagnoses failed: %v", err)
	}
	if records == nil {
		t.Fatal("expected empty slice (not nil)")
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestDiagnosis_ListDefaults(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	records, err := store.ListDiagnoses(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("ListDiagnoses failed: %v", err)
	}
	if records == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
}

// ─── Ping / Close ───────────────────────────────────────────

func TestStore_Ping(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestStore_OperationsAfterClose(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Close the store
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Operations after close should fail
	err := store.SavePipelineResult(ctx, testPipelineResult("after-close"))
	if err == nil {
		t.Error("expected error when saving after close")
	}

	_, err = store.ListPipelineResults(ctx, 10, 0)
	if err == nil {
		t.Error("expected error when listing after close")
	}
}

func TestStore_DoubleClose(t *testing.T) {
	store := newTestStore(t)

	if err := store.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	// Double close should not panic
	store.Close()
}
