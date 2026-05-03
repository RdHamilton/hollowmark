package logreader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Snapshot copies src to archiveDir/Player_<timestamp>.log before MTGA can overwrite it.
// Returns ("", nil) if src does not exist.
func Snapshot(src, archiveDir string) (string, error) {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", nil
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	dst := filepath.Join(archiveDir, fmt.Sprintf("Player_%s.log", ts))

	if err := copyFile(src, dst); err != nil {
		return "", fmt.Errorf("copy log: %w", err)
	}
	return dst, nil
}

// ListSnapshots returns all snapshot paths in archiveDir, sorted oldest-first by filename.
// Returns nil if archiveDir does not exist.
func ListSnapshots(archiveDir string) ([]string, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read archive dir: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "Player_") && strings.HasSuffix(e.Name(), ".log") {
			paths = append(paths, filepath.Join(archiveDir, e.Name()))
		}
	}
	return paths, nil
}

// PruneSnapshots removes snapshots in archiveDir whose modification time is older than maxAge.
func PruneSnapshots(archiveDir string, maxAge time.Duration) error {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read archive dir: %w", err)
	}

	cutoff := time.Now().UTC().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "Player_") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(archiveDir, e.Name()))
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return out.Sync()
}
