package artifact

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kexer2018/miniflow/internal/db"
)

type memoryStore struct{ records map[string]db.ArtifactRecord }

func (s *memoryStore) key(runID, name string) string { return runID + "/" + name }
func (s *memoryStore) SaveArtifact(_ context.Context, record db.ArtifactRecord) error {
	s.records[s.key(record.RunID, record.Name)] = record
	return nil
}
func (s *memoryStore) GetArtifact(_ context.Context, runID, name string) (*db.ArtifactRecord, error) {
	record, ok := s.records[s.key(runID, name)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &record, nil
}
func (s *memoryStore) ListArtifacts(_ context.Context, runID string) ([]db.ArtifactRecord, error) {
	var records []db.ArtifactRecord
	for _, record := range s.records {
		if record.RunID == runID {
			records = append(records, record)
		}
	}
	return records, nil
}

func TestSaveAndRestore(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "dist", "app.txt"), []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{records: make(map[string]db.ArtifactRecord)}
	manager := NewManager(filepath.Join(t.TempDir(), "artifacts"), store)

	record, err := manager.Save(context.Background(), "run-1", "build", workspace, "dist", "error")
	if err != nil {
		t.Fatalf("save artifact: %v", err)
	}
	if record.Size == 0 {
		t.Fatal("expected archive data")
	}

	restoreWorkspace := t.TempDir()
	if _, err := manager.Restore(context.Background(), "run-1", "build", restoreWorkspace, "."); err != nil {
		t.Fatalf("restore artifact: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(restoreWorkspace, "dist", "app.txt"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "release" {
		t.Fatalf("got %q, want release", data)
	}
}
