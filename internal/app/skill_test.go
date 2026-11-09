package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/layout"
	"github.com/ayame113/my-workspace-manager/skills"
)

func homeApp(t *testing.T) (*app.App, string) {
	t.Helper()
	home := t.TempDir()
	return app.New(app.Config{
		Layout: layout.Layout{Root: filepath.Join(home, "ws")},
		Getenv: func(name string) string {
			if name == "HOME" {
				return home
			}
			return ""
		},
	}), home
}

func TestInstallSkillsWritesEachTarget(t *testing.T) {
	t.Parallel()
	want, err := skills.FS.ReadFile("ws-task/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for target, dir := range map[string]string{"claude": ".claude/skills", "codex": ".agents/skills"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			a, home := homeApp(t)
			skillFile := filepath.Join(home, dir, "ws-task/SKILL.md")
			if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(skillFile, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			installed, err := a.InstallSkills(target)
			if err != nil {
				t.Fatal(err)
			}
			if len(installed) != 1 || installed[0] != filepath.Dir(skillFile) {
				t.Errorf("installed = %v", installed)
			}
			got, err := os.ReadFile(skillFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Error("SKILL.md was not overwritten with the bundled one")
			}
		})
	}
}

func TestInstallSkillsRejectsUnknownTarget(t *testing.T) {
	t.Parallel()
	a, _ := homeApp(t)
	if _, err := a.InstallSkills("bogus"); err == nil {
		t.Error("want an error")
	}
}
