package ait

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mattn/go-isatty"

	aitfiles "github.com/ohnotnow/agent-issue-tracker"
)

// The skills, agents and commands that go with this ait are built into the
// binary (claudefiles.go at the repo root), so a binary and its skills always
// come from the same release. `install-skills` copies them into ~/.claude;
// `version` reports when the installed copies differ.

// skillFiles is the embedded claude/ folder, as paths relative to it
// (skills/ait/SKILL.md, agents/plan-to-ait.md, commands/hello-ait.md).
var skillFiles fs.FS = mustSub(aitfiles.Claude, "claude")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// skillFile is one embedded file and where it would be installed.
type skillFile struct {
	rel     string // skills/ait/SKILL.md
	content []byte
	dest    string
}

func skillFilesFor(files fs.FS, claudeDir string) ([]skillFile, error) {
	var out []skillFile
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		out = append(out, skillFile{rel: path, content: b, dest: filepath.Join(claudeDir, filepath.FromSlash(path))})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, err
}

// installed says how the copy at f.dest compares: "missing", "same" or "differs".
func (f skillFile) installed() string {
	b, err := os.ReadFile(f.dest)
	switch {
	case err != nil:
		return "missing"
	case bytes.Equal(b, f.content):
		return "same"
	}
	return "differs"
}

func defaultClaudeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// skillsStatus is one line for `ait version`: whether the installed skills
// match the ones built into this binary.
func skillsStatus(files fs.FS, claudeDir string) string {
	list, err := skillFilesFor(files, claudeDir)
	if err != nil {
		return "skills: couldn't read the built-in copies: " + err.Error()
	}
	var missing, differ []string
	for _, f := range list {
		switch f.installed() {
		case "missing":
			missing = append(missing, f.rel)
		case "differs":
			differ = append(differ, f.rel)
		}
	}
	switch {
	case len(missing) == len(list):
		return fmt.Sprintf("skills: not installed in %s; run ait install-skills", claudeDir)
	case len(missing)+len(differ) == 0:
		return fmt.Sprintf("skills: up to date in %s", claudeDir)
	}
	return fmt.Sprintf("skills: %s in %s differ from this ait's; run ait install-skills to see and update them",
		strings.Join(append(differ, missing...), ", "), claudeDir)
}

// installConfig is install-skills' moving parts, so tests can drive it.
type installConfig struct {
	files       fs.FS
	claudeDir   string
	force       bool
	dryRun      bool
	interactive bool // stdin is a terminal, so we may ask
	stdin       io.Reader
	stdout      io.Writer
}

// RunInstallSkills handles 'ait install-skills'.
func RunInstallSkills(args []string) error {
	fs := flag.NewFlagSet("install-skills", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", defaultClaudeDir(), "")
	force := fs.Bool("force", false, "")
	dryRun := fs.Bool("dry-run", false, "")
	if helped, err := parseFlags(fs, args, "install-skills"); helped || err != nil {
		return err
	}
	return installSkills(installConfig{
		files: skillFiles, claudeDir: *dir, force: *force, dryRun: *dryRun,
		interactive: isTerminal(os.Stdin), stdin: os.Stdin, stdout: os.Stdout,
	})
}

// isTerminal reports whether r is a terminal someone can type at. It asks
// the terminal driver, not the file mode: /dev/null is a character device
// too, and it is exactly what an agent's stdin often is.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd()))
}

// installSkills copies each built-in file into place. A new file is
// written; an identical one is left alone; a changed one is replaced only
// with --force or a yes at the prompt ([y/N/d], d shows the diff). With no
// terminal to ask at, a changed file is left as it is and reported.
func installSkills(cfg installConfig) error {
	list, err := skillFilesFor(cfg.files, cfg.claudeDir)
	if err != nil {
		return err
	}
	out := cfg.stdout
	if cfg.dryRun {
		fmt.Fprintf(out, "Dry run: nothing will be written. Comparing with %s:\n", cfg.claudeDir)
	} else {
		fmt.Fprintf(out, "Installing the skills, agents and commands from ait %s into %s:\n", Version, cfg.claudeDir)
	}
	reader := bufio.NewReader(cfg.stdin)
	var left []string
	for _, f := range list {
		state := f.installed()
		switch {
		case state == "same":
			fmt.Fprintf(out, "  %s: unchanged\n", f.rel)
			continue
		case cfg.dryRun && state == "missing":
			fmt.Fprintf(out, "  %s: would be added\n", f.rel)
			continue
		case cfg.dryRun:
			fmt.Fprintf(out, "  %s: differs, would ask before replacing\n", f.rel)
			continue
		case state == "differs" && !cfg.force && !cfg.interactive:
			fmt.Fprintf(out, "  %s: differs, left as it is (no terminal to ask at; --force replaces it)\n", f.rel)
			left = append(left, f.rel)
			continue
		case state == "differs" && !cfg.force:
			yes, err := askReplace(out, reader, f)
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintf(out, "  %s: kept your copy\n", f.rel)
				left = append(left, f.rel)
				continue
			}
		}
		if err := writeSkillFile(f); err != nil {
			return err
		}
		verb := "added"
		if state == "differs" {
			verb = "replaced"
		}
		fmt.Fprintf(out, "  %s: %s\n", f.rel, verb)
	}
	if len(left) > 0 && !cfg.dryRun {
		fmt.Fprintf(out, "Left as they were: %s.\n", strings.Join(left, ", "))
	}
	return nil
}

// errAborted ends install-skills when the input runs out mid-prompt, so an
// end of input can never be taken as an answer to the questions after it.
var errAborted = errors.New("aborted: the input ended before every question was answered; nothing more was changed")

func askReplace(out io.Writer, in *bufio.Reader, f skillFile) (bool, error) {
	fmt.Fprintf(out, "  %s differs from the copy in ait %s.\n", f.dest, Version)
	for {
		fmt.Fprint(out, "  Replace it with ait's? [y/N/d] ")
		line, err := in.ReadString('\n')
		if err != nil && (err != io.EOF || line == "") {
			fmt.Fprintln(out)
			return false, errAborted
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "d", "diff":
			showDiff(out, f)
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
}

// showDiff prints `diff -u` of the installed copy against ait's.
func showDiff(out io.Writer, f skillFile) {
	tmp, err := os.CreateTemp("", "ait-skill-*")
	if err != nil {
		fmt.Fprintf(out, "  (can't show a diff: %v)\n", err)
		return
	}
	defer os.Remove(tmp.Name())
	tmp.Write(f.content)
	tmp.Close()
	cmd := exec.Command("diff", "-u", "--label", "installed "+f.rel, "--label", "ait "+Version+" "+f.rel, f.dest, tmp.Name())
	b, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		fmt.Fprintf(out, "  (can't show a diff: %v)\n", err)
		return
	}
	out.Write(b)
}

func writeSkillFile(f skillFile) error {
	if err := os.MkdirAll(filepath.Dir(f.dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f.dest, f.content, 0o644)
}

// checkNewSkills runs after self-update swaps the binary in. It asks the new
// binary, which carries the new skills, how they compare with the installed
// ones, so the user learns at once whether the skills changed too.
func checkNewSkills(target string, out io.Writer) {
	fmt.Fprintln(out, "\nThe skills, agents and commands may have changed too:")
	cmd := exec.Command(target, "install-skills", "--dry-run")
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(out, "(couldn't run the new ait to check: %v)\n", err)
		return
	}
	fmt.Fprintln(out, "Run ait install-skills to bring them up to date.")
}
