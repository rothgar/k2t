package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationState_PhaseTracking(t *testing.T) {
	s := &MigrationState{
		Phases: make(map[string]bool),
	}

	phases := []string{"COLLECT", "CONFIRM", "GENERATE", "DEPLOY", "BOOTSTRAP"}
	for _, p := range phases {
		if s.PhaseCompleted(p) {
			t.Errorf("phase %s should not be complete before marking", p)
		}
		s.MarkPhaseComplete(p)
		if !s.PhaseCompleted(p) {
			t.Errorf("phase %s should be complete after marking", p)
		}
	}
}

func TestMigrationState_MarkPhaseInitializesMap(t *testing.T) {
	// MarkPhaseComplete should not panic even if Phases is nil.
	s := &MigrationState{}
	s.MarkPhaseComplete("COLLECT")
	if !s.PhaseCompleted("COLLECT") {
		t.Error("phase should be complete even when Phases was nil before marking")
	}
}

func TestMigrationState_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &MigrationState{
		Host:      "192.168.1.100",
		BackupDir: dir,
		Phases:    map[string]bool{"COLLECT": true, "GENERATE": true},
	}
	if err := original.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := loadOrInitState(path, "192.168.1.100")
	if err != nil {
		t.Fatalf("loadOrInitState failed: %v", err)
	}

	if !loaded.PhaseCompleted("COLLECT") {
		t.Error("COLLECT should be complete after reload")
	}
	if !loaded.PhaseCompleted("GENERATE") {
		t.Error("GENERATE should be complete after reload")
	}
	if loaded.PhaseCompleted("DEPLOY") {
		t.Error("DEPLOY should not be complete")
	}
}

func TestLoadOrInitState_HostMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &MigrationState{
		Host:   "192.168.1.100",
		Phases: map[string]bool{"COLLECT": true},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}

	// Loading with a different host should return a fresh state.
	loaded, err := loadOrInitState(path, "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PhaseCompleted("COLLECT") {
		t.Error("state from a different host should not carry over completed phases")
	}
}

func TestLoadOrInitState_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	s, err := loadOrInitState(path, "10.0.0.1")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s == nil || s.Phases == nil {
		t.Error("expected a valid initial state")
	}
}

func TestMigrationState_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &MigrationState{Host: "test", Phases: map[string]bool{}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// State file should be readable only by owner (mode 0600).
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}
}
