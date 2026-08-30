package ait

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RunSelfUpdate handles 'ait self-update'. It downloads the latest release
// binary for the current platform, verifies it against the published
// SHA256SUMS, and atomically replaces the running executable.
//
// Flags:
//
//	--check    Report whether an update is available without downloading.
//	           Exits 0 when current, 1 when newer is available, 2 on
//	           lookup failure. Mirrors the 'composer outdated' style.
//	--yes/-y   Skip the confirmation prompt.
func RunSelfUpdate(args []string) error {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var check, yes bool
	fs.BoolVar(&check, "check", false, "")
	fs.BoolVar(&yes, "yes", false, "")
	fs.BoolVar(&yes, "y", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			PrintCommandHelp("self-update")
			return nil
		}
		return &CLIError{Code: "usage", Message: err.Error(), ExitCode: 64}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if real, lerr := filepath.EvalSymlinks(exe); lerr == nil {
		exe = real
	}

	home, _ := os.UserHomeDir()

	return runSelfUpdate(selfUpdateConfig{
		apiURL:         buildAPIURL(RepoURL),
		httpClient:     &http.Client{Timeout: 60 * time.Second},
		targetPath:     exe,
		osName:         runtime.GOOS,
		arch:           runtime.GOARCH,
		stdin:          os.Stdin,
		stdout:         os.Stdout,
		stderr:         os.Stderr,
		currentVersion: Version,
		gopath:         os.Getenv("GOPATH"),
		home:           home,
		checkOnly:      check,
		assumeYes:      yes,
	})
}

// selfUpdateConfig bundles the moving parts of a self-update run so tests
// can substitute an httptest server, a temp file standing in for the
// running binary, and a known platform tuple.
type selfUpdateConfig struct {
	apiURL         string
	httpClient     *http.Client
	targetPath     string
	osName         string
	arch           string
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	currentVersion string
	gopath         string
	home           string
	checkOnly      bool
	assumeYes      bool
}

func runSelfUpdate(cfg selfUpdateConfig) error {
	// Dev builds short-circuit before any network call. Replacing a
	// hand-built dev binary with a release one is almost never what the
	// user wants, and there is no version to compare against anyway.
	if cfg.currentVersion == "dev" {
		if cfg.checkOnly {
			fmt.Fprintln(cfg.stdout, "ait is a dev build — version check skipped.")
			return nil
		}
		fmt.Fprintln(cfg.stdout, "ait is a dev build — self-update is disabled.")
		fmt.Fprintln(cfg.stdout, "Rebuild from source, or install a release binary.")
		return nil
	}

	rel, err := fetchLatestRelease(cfg.httpClient, cfg.apiURL)
	if err != nil {
		// In --check mode, lookup failures get the dedicated exit-2 code
		// so scripts can distinguish "couldn't find out" from "an update
		// is available". Outside --check, fall through to the default
		// exit-1 path.
		wrapped := fmt.Errorf("fetch latest release: %w", err)
		if cfg.checkOnly {
			return ExitWithCode(2, wrapped)
		}
		return wrapped
	}

	if !isNewer(rel.TagName, cfg.currentVersion) {
		fmt.Fprintf(cfg.stdout, "Already up to date (%s).\n", cfg.currentVersion)
		return nil
	}

	// --check stops here: report and exit 1 silently. No download, no
	// prompt, no swap — just a non-zero exit so scripts can act on it.
	if cfg.checkOnly {
		fmt.Fprintf(cfg.stdout, "ait %s is installed; %s is available.\n",
			cfg.currentVersion, rel.TagName)
		return ExitWithCode(1, nil)
	}

	// A newer version exists. Before we touch anything, ask whether this
	// binary even belongs to us — Homebrew and 'go install' manage their
	// own copies, and a self-update would silently sidestep them.
	pmHint, pmAct := detectPackageManager(cfg.targetPath, cfg.osName, cfg.gopath, cfg.home)
	if pmAct == pmRedirect {
		fmt.Fprintf(cfg.stdout, "ait was installed via %s.\n", pmHint.manager)
		fmt.Fprintf(cfg.stdout, "A newer version (%s) is available — update with:\n  %s\n", rel.TagName, pmHint.command)
		return nil
	}

	assetName := assetNameFor(cfg.osName, cfg.arch)
	if assetName == "" {
		return &CLIError{
			Code:     "validation",
			Message:  fmt.Sprintf("no release asset for platform %s/%s", cfg.osName, cfg.arch),
			ExitCode: 65,
		}
	}

	binAsset, ok := findAsset(rel.Assets, assetName)
	if !ok {
		return &CLIError{
			Code:     "not_found",
			Message:  fmt.Sprintf("release %s does not include asset %q", rel.TagName, assetName),
			ExitCode: 66,
		}
	}
	sumsAsset, ok := findAsset(rel.Assets, "SHA256SUMS")
	if !ok {
		return &CLIError{
			Code:     "not_found",
			Message:  fmt.Sprintf("release %s does not include SHA256SUMS", rel.TagName),
			ExitCode: 66,
		}
	}

	// Bail out before download if we can't write to the install dir.
	// Probing now (rather than failing at rename time) means a sudo'd
	// install location reports immediately instead of after a multi-MB
	// download.
	dir := filepath.Dir(cfg.targetPath)
	if !canWrite(dir) {
		return &CLIError{
			Code: "validation",
			Message: fmt.Sprintf(
				"cannot write to %s — re-run with sudo, or visit %s/releases/latest",
				dir, RepoURL),
			ExitCode: 65,
		}
	}

	if !cfg.assumeYes {
		ok, err := confirmUpdate(cfg, rel, assetName)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cfg.stdout, "Aborted.")
			return nil
		}
	}

	binBytes, err := downloadAsset(cfg.httpClient, binAsset.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", assetName, err)
	}
	sumsBytes, err := downloadAsset(cfg.httpClient, sumsAsset.URL)
	if err != nil {
		return fmt.Errorf("download SHA256SUMS: %w", err)
	}

	expected, err := lookupChecksum(string(sumsBytes), assetName)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(binBytes)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return &CLIError{
			Code:     "validation",
			Message:  fmt.Sprintf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual),
			ExitCode: 65,
		}
	}

	if pmAct == pmWarn && cfg.stderr != nil {
		fmt.Fprintf(cfg.stderr,
			"warning: ait lives in %s, which is normally managed by your system package manager.\n",
			filepath.Dir(cfg.targetPath))
		fmt.Fprintln(cfg.stderr, "self-update will sidestep that — proceeding anyway.")
	}

	if err := atomicSwap(cfg.targetPath, binBytes); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Fprintf(cfg.stdout, "Updated to %s.\n", rel.TagName)
	return nil
}

// confirmUpdate prints a summary, the release notes, and a y/N prompt,
// then reads a single line from stdin. Default (empty input or anything
// not 'y'/'yes') is no.
func confirmUpdate(cfg selfUpdateConfig, rel *ghRelease, assetName string) (bool, error) {
	fmt.Fprintf(cfg.stdout, "ait %s -> %s\n", cfg.currentVersion, rel.TagName)
	fmt.Fprintf(cfg.stdout, "Asset: %s\n", assetName)

	body := strings.TrimSpace(rel.Body)
	if body != "" {
		fmt.Fprintln(cfg.stdout)
		fmt.Fprintln(cfg.stdout, "Release notes:")
		fmt.Fprintln(cfg.stdout, body)
	}

	fmt.Fprintln(cfg.stdout)
	fmt.Fprint(cfg.stdout, "Proceed? [y/N]: ")

	if cfg.stdin == nil {
		return false, nil
	}
	reader := bufio.NewReader(cfg.stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// pmAction classifies what self-update should do when the running binary
// looks like it was installed by a package manager.
type pmAction int

const (
	pmNone     pmAction = iota // self-installed; proceed normally
	pmRedirect                 // package manager owns this; print hint and exit
	pmWarn                     // system bin we'll touch anyway, but warn
)

// pmHint is the human-facing pair of strings emitted alongside a redirect.
type pmHint struct {
	manager string
	command string
}

// detectPackageManager classifies path against well-known install locations.
// goos, gopath, and home are passed in so tests can drive the function with
// known values without touching real environment variables.
//
// Heuristics — not gates. False positives just route the user to a tool that
// already does the right thing; false negatives fall through to a regular
// self-update.
func detectPackageManager(path, goos, gopath, home string) (pmHint, pmAction) {
	p := filepath.ToSlash(path)

	if strings.HasPrefix(p, "/opt/homebrew/") ||
		strings.Contains(p, "/Cellar/") ||
		strings.Contains(p, "/homebrew/") ||
		strings.Contains(p, "/linuxbrew/") {
		return pmHint{manager: "Homebrew", command: "brew upgrade ait"}, pmRedirect
	}

	goBin := ""
	switch {
	case gopath != "":
		goBin = filepath.ToSlash(filepath.Join(gopath, "bin"))
	case home != "":
		goBin = filepath.ToSlash(filepath.Join(home, "go", "bin"))
	}
	if goBin != "" && (p == goBin || strings.HasPrefix(p, goBin+"/")) {
		return pmHint{
			manager: "'go install'",
			command: "go install github.com/ohnotnow/agent-issue-tracker/cmd/ait@latest",
		}, pmRedirect
	}

	if goos == "linux" {
		dir := filepath.ToSlash(filepath.Dir(p))
		if dir == "/usr/bin" || dir == "/usr/local/bin" {
			return pmHint{}, pmWarn
		}
	}

	return pmHint{}, pmNone
}

// canWrite reports whether the calling process can create files in dir.
// Implemented by trying to create (and immediately remove) a tempfile —
// the only portable answer that works across POSIX and Windows.
func canWrite(dir string) bool {
	f, err := os.CreateTemp(dir, ".ait-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// assetNameFor returns the release asset filename ait publishes for a given
// GOOS/GOARCH, or "" if the platform isn't built. Mirrors the workflow's
// build matrix in .github/workflows/release.yml — keep the two in sync.
func assetNameFor(goos, goarch string) string {
	switch goos {
	case "windows":
		if goarch == "amd64" || goarch == "arm64" {
			return "ait-windows-" + goarch + ".exe"
		}
	case "darwin":
		if goarch == "amd64" || goarch == "arm64" {
			return "ait-darwin-" + goarch
		}
	case "linux":
		if goarch == "amd64" || goarch == "arm64" {
			return "ait-linux-" + goarch
		}
	}
	return ""
}

func findAsset(assets []ghAsset, name string) (ghAsset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return ghAsset{}, false
}

func downloadAsset(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// lookupChecksum scans a SHA256SUMS file for a line matching name and
// returns its hex digest. Each line is '<hex><space><space-or-asterisk><name>'
// (binary mode uses two spaces, text mode uses ' *'); strings.Fields
// flattens both into the same two-field split.
func lookupChecksum(sums, name string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		fname := strings.TrimPrefix(fields[1], "*")
		if fname == name {
			return fields[0], nil
		}
	}
	return "", &CLIError{
		Code:     "not_found",
		Message:  fmt.Sprintf("no checksum entry for %q in SHA256SUMS", name),
		ExitCode: 66,
	}
}

// atomicSwap replaces the file at target with content. On POSIX it writes a
// sibling temp file and renames over the target — atomic at the filesystem
// level. On Windows the running .exe cannot be overwritten, so the current
// binary is renamed to <target>.old first; that file is left behind for the
// OS or the next self-update invocation to clean up.
func atomicSwap(target string, content []byte) error {
	dir := filepath.Dir(target)
	base := filepath.Base(target)

	tmp, err := os.CreateTemp(dir, base+".new-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		oldPath := target + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(target, oldPath); err != nil {
			return err
		}
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}
