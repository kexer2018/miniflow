// Package artifact manages local, run-scoped artifact archives.
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kexer2018/miniflow/internal/db"
)

// Manager stores archive bytes on the local filesystem and metadata in an
// optional database index.
type Manager struct {
	Root  string
	store db.ArtifactStore
}

func NewManager(root string, store db.ArtifactStore) *Manager {
	return &Manager{Root: root, store: store}
}

func (m *Manager) Save(ctx context.Context, runID, name, workspace, pattern, ifNoFiles string) (db.ArtifactRecord, error) {
	if err := validateName(name); err != nil {
		return db.ArtifactRecord{}, err
	}
	path, err := workspacePattern(workspace, pattern)
	if err != nil {
		return db.ArtifactRecord{}, err
	}
	matches, err := filepath.Glob(path)
	if err != nil {
		return db.ArtifactRecord{}, fmt.Errorf("glob artifact path: %w", err)
	}
	if len(matches) == 0 {
		switch ifNoFiles {
		case "ignore", "warn":
			return db.ArtifactRecord{}, nil
		default:
			return db.ArtifactRecord{}, fmt.Errorf("artifact %q matched no files", name)
		}
	}

	dir := filepath.Join(m.Root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return db.ArtifactRecord{}, fmt.Errorf("create artifact directory: %w", err)
	}
	archivePath := filepath.Join(dir, name+".tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		return db.ArtifactRecord{}, fmt.Errorf("create artifact archive: %w", err)
	}
	writeErr := writeArchive(file, workspace, matches)
	closeErr := file.Close()
	if writeErr != nil {
		return db.ArtifactRecord{}, writeErr
	}
	if closeErr != nil {
		return db.ArtifactRecord{}, fmt.Errorf("close artifact archive: %w", closeErr)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return db.ArtifactRecord{}, fmt.Errorf("stat artifact archive: %w", err)
	}
	record := db.ArtifactRecord{RunID: runID, Name: name, Path: archivePath, Size: info.Size(), CreatedAt: time.Now()}
	if m.store != nil {
		if err := m.store.SaveArtifact(ctx, record); err != nil {
			return db.ArtifactRecord{}, err
		}
	}
	return record, nil
}

func (m *Manager) Restore(ctx context.Context, runID, name, workspace, target string) (db.ArtifactRecord, error) {
	if m.store == nil {
		return db.ArtifactRecord{}, fmt.Errorf("artifact metadata store is not configured")
	}
	record, err := m.store.GetArtifact(ctx, runID, name)
	if err != nil {
		return db.ArtifactRecord{}, err
	}
	targetPath, err := workspacePath(workspace, target)
	if err != nil {
		return db.ArtifactRecord{}, err
	}
	if err := extractArchive(record.Path, targetPath); err != nil {
		return db.ArtifactRecord{}, err
	}
	return *record, nil
}

func (m *Manager) List(ctx context.Context, runID string) ([]db.ArtifactRecord, error) {
	if m.store == nil {
		return nil, fmt.Errorf("artifact metadata store is not configured")
	}
	return m.store.ListArtifacts(ctx, runID)
}

func (m *Manager) Get(ctx context.Context, runID, name string) (*db.ArtifactRecord, error) {
	if m.store == nil {
		return nil, fmt.Errorf("artifact metadata store is not configured")
	}
	return m.store.GetArtifact(ctx, runID, name)
}

func writeArchive(output io.Writer, workspace string, matches []string) error {
	gz := gzip.NewWriter(output)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	seen := make(map[string]struct{})
	for _, match := range matches {
		err := filepath.Walk(match, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			rel, err := filepath.Rel(workspace, path)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				return fmt.Errorf("artifact path escapes workspace: %q", path)
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(rel)
			if info.IsDir() {
				header.Name += "/"
			}
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				_, err = io.Copy(tw, file)
				closeErr := file.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("archive artifact: %w", err)
		}
	}
	return nil
}

func extractArchive(archivePath, target string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open artifact archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read artifact archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read artifact entry: %w", err)
		}
		dest, err := workspacePath(target, header.Name)
		if err != nil {
			return err
		}
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, header.FileInfo().Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
}

func workspacePattern(workspace, pattern string) (string, error) {
	return workspacePath(workspace, pattern)
}

func workspacePath(workspace, value string) (string, error) {
	if value == "." {
		return workspace, nil
	}
	if value == "" || filepath.IsAbs(value) {
		return "", fmt.Errorf("path must be relative to the workspace")
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %q", value)
	}
	return filepath.Join(workspace, clean), nil
}

func validateName(name string) error {
	if name == "" || filepath.Base(name) != name || name == "." {
		return fmt.Errorf("invalid artifact name %q", name)
	}
	return nil
}
