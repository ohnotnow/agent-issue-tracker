package ait

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// The skills, agents and commands are built into the binary (claudefiles.go),
// so install-skills puts this release's copies in place and version says when
// the installed ones differ.

var testSkills = fstest.MapFS{
	"skills/ait/SKILL.md":   {Data: []byte("ait skill v2\n")},
	"agents/plan-to-ait.md": {Data: []byte("plan agent v2\n")},
	"commands/hello-ait.md": {Data: []byte("hello v2\n")},
}

func install(t *testing.T, cfg installConfig) (string, error) {
	t.Helper()
	var out bytes.Buffer
	if cfg.files == nil {
		cfg.files = testSkills
	}
	if cfg.stdin == nil {
		cfg.stdin = strings.NewReader("")
	}
	cfg.stdout = &out
	err := installSkills(cfg)
	return out.String(), err
}

func readOr(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestInstallingSkills(t *testing.T) {
	t.Run("the real build carries the skills, agents and command, and not ait-fleet", func(t *testing.T) {
		list, err := skillFilesFor(skillFiles, "x")
		if err != nil {
			t.Fatal(err)
		}
		var rels []string
		for _, f := range list {
			rels = append(rels, f.rel)
		}
		want := []string{
			"agents/ait-amnesia-check.md",
			"agents/ait-audit.md",
			"agents/plan-to-ait.md",
			"commands/hello-ait.md",
			"skills/ait-expand-issue/SKILL.md",
			"skills/ait-feature-workflow/SKILL.md",
			"skills/ait-recap/SKILL.md",
			"skills/ait/DELEGATION.md",
			"skills/ait/SKILL.md",
		}
		if strings.Join(rels, " ") != strings.Join(want, " ") {
			t.Fatalf("embedded = %v, want %v", rels, want)
		}
	})

	t.Run("missing files are added and identical ones left alone", func(t *testing.T) {
		dir := t.TempDir()
		out, err := install(t, installConfig{claudeDir: dir})
		if err != nil || readOr(t, filepath.Join(dir, "commands/hello-ait.md")) != "hello v2\n" {
			t.Fatalf("err %v, out:\n%s", err, out)
		}
		out, _ = install(t, installConfig{claudeDir: dir})
		if strings.Count(out, "unchanged") != 3 {
			t.Fatalf("second run:\n%s", out)
		}
	})

	t.Run("with no terminal, a changed file is left as it is and reported", func(t *testing.T) {
		dir := t.TempDir()
		install(t, installConfig{claudeDir: dir})
		mine := filepath.Join(dir, "agents/plan-to-ait.md")
		os.WriteFile(mine, []byte("my tuned agent\n"), 0o644)
		out, err := install(t, installConfig{claudeDir: dir})
		if err != nil || readOr(t, mine) != "my tuned agent\n" || !strings.Contains(out, "agents/plan-to-ait.md: differs, left as it is") {
			t.Fatalf("err %v, file %q, out:\n%s", err, readOr(t, mine), out)
		}
	})

	t.Run("at a terminal: N keeps it, d shows the diff and asks again, y replaces it", func(t *testing.T) {
		dir := t.TempDir()
		install(t, installConfig{claudeDir: dir})
		mine := filepath.Join(dir, "agents/plan-to-ait.md")
		os.WriteFile(mine, []byte("my tuned agent\n"), 0o644)

		out, _ := install(t, installConfig{claudeDir: dir, interactive: true, stdin: strings.NewReader("\n")})
		if readOr(t, mine) != "my tuned agent\n" || !strings.Contains(out, "kept your copy") {
			t.Fatalf("N: file %q, out:\n%s", readOr(t, mine), out)
		}
		out, _ = install(t, installConfig{claudeDir: dir, interactive: true, stdin: strings.NewReader("d\ny\n")})
		if readOr(t, mine) != "plan agent v2\n" || !strings.Contains(out, "-my tuned agent") || !strings.Contains(out, "+plan agent v2") || strings.Count(out, "[y/N/d]") != 2 {
			t.Fatalf("d then y: file %q, out:\n%s", readOr(t, mine), out)
		}
	})

	t.Run("input ending mid-prompt stops, and never counts as an answer", func(t *testing.T) {
		dir := t.TempDir()
		install(t, installConfig{claudeDir: dir})
		for _, f := range []string{"agents/plan-to-ait.md", "skills/ait/SKILL.md"} {
			os.WriteFile(filepath.Join(dir, f), []byte("mine\n"), 0o644)
		}
		_, err := install(t, installConfig{claudeDir: dir, interactive: true, stdin: strings.NewReader("")})
		if err != errAborted || readOr(t, filepath.Join(dir, "skills/ait/SKILL.md")) != "mine\n" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("--force replaces without asking, and --dry-run changes nothing", func(t *testing.T) {
		dir := t.TempDir()
		install(t, installConfig{claudeDir: dir})
		mine := filepath.Join(dir, "agents/plan-to-ait.md")
		os.WriteFile(mine, []byte("mine\n"), 0o644)
		os.Remove(filepath.Join(dir, "commands/hello-ait.md"))
		out, _ := install(t, installConfig{claudeDir: dir, dryRun: true})
		if readOr(t, mine) != "mine\n" || readOr(t, filepath.Join(dir, "commands/hello-ait.md")) != "<missing>" ||
			!strings.Contains(out, "differs, would ask") || !strings.Contains(out, "would be added") {
			t.Fatalf("dry run: out:\n%s", out)
		}
		install(t, installConfig{claudeDir: dir, force: true})
		if readOr(t, mine) != "plan agent v2\n" {
			t.Fatalf("force: file %q", readOr(t, mine))
		}
	})

	t.Run("files ait doesn't ship are never touched", func(t *testing.T) {
		dir := t.TempDir()
		retired := filepath.Join(dir, "skills/ait-crafting/SKILL.md")
		os.MkdirAll(filepath.Dir(retired), 0o755)
		os.WriteFile(retired, []byte("old craft\n"), 0o644)
		install(t, installConfig{claudeDir: dir, force: true})
		if readOr(t, retired) != "old craft\n" {
			t.Fatalf("retired skill = %q", readOr(t, retired))
		}
	})

	t.Run("version says whether the installed skills match", func(t *testing.T) {
		dir := t.TempDir()
		if s := skillsStatus(testSkills, dir); !strings.Contains(s, "not installed") {
			t.Fatal(s)
		}
		install(t, installConfig{claudeDir: dir})
		if s := skillsStatus(testSkills, dir); !strings.Contains(s, "up to date") {
			t.Fatal(s)
		}
		os.WriteFile(filepath.Join(dir, "agents/plan-to-ait.md"), []byte("old\n"), 0o644)
		if s := skillsStatus(testSkills, dir); !strings.Contains(s, "agents/plan-to-ait.md") || !strings.Contains(s, "ait install-skills") {
			t.Fatal(s)
		}
	})
}
