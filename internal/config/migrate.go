package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MigrateFromPython copies the Python snooze-server YAML files from srcDir
// into the layout that the Go server expects under dstDir. The renames map is
// intentionally identical to the “sectionFiles“ table so that operators can
// keep using the existing filenames while opting in to the new ones over time.
//
// This is a placeholder implementation. It copies whatever recognised YAML
// files are present and leaves a TODO marker for the per-field translation
// work that will land alongside the “settings“ plugin. It is not yet wired into the
// “snooze-server migrate-config“ subcommand, which is currently a deferred
// stub (see runMigrateConfig); the loader already accepts the legacy file
// names verbatim via the shared sectionFiles table, so the standalone
// migration step is on the roadmap rather than the critical path.
func MigrateFromPython(srcDir, dstDir string) error {
	if srcDir == "" {
		return fmt.Errorf("config: migrate: source directory is empty")
	}
	if dstDir == "" {
		return fmt.Errorf("config: migrate: destination directory is empty")
	}
	if err := os.MkdirAll(dstDir, 0o750); err != nil {
		return fmt.Errorf("config: migrate: mkdir %q: %w", dstDir, err)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("config: migrate: read %q: %w", srcDir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		base := name[:len(name)-len(ext)]
		section, ok := sectionFiles[base]
		if !ok {
			continue
		}
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(dstDir, section+".yaml")
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("config: migrate: copy %s -> %s: %w", srcPath, dstPath, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
