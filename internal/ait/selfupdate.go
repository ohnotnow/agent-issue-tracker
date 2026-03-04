package ait

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repoURL = "https://github.com/ohnotnow/agent-issue-tracker"

func SelfUpdate(currentVersion string) error {
	if currentVersion == "dev" {
		return &CLIError{
			Code:    "self_update",
			Message: "self-update requires a release build (version is \"dev\")",
		}
	}

	latestTag, err := fetchLatestTag()
	if err != nil {
		return fmt.Errorf("checking latest version: %w", err)
	}

	if latestTag == currentVersion {
		return PrintJSON(map[string]any{
			"status":  "up_to_date",
			"version": currentVersion,
		})
	}

	asset := assetName()
	downloadURL := fmt.Sprintf("%s/releases/download/%s/%s", repoURL, latestTag, asset)

	fmt.Fprintf(os.Stderr, "Updating %s -> %s ...\n", currentVersion, latestTag)

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolving symlinks: %w", err)
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", asset, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", asset, resp.StatusCode)
	}

	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".ait-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("setting permissions: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replacing binary: %w", err)
	}

	return PrintJSON(map[string]any{
		"status":      "updated",
		"old_version": currentVersion,
		"new_version": latestTag,
	})
}

func fetchLatestTag() (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(repoURL + "/releases/latest")
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("unexpected status %d from releases/latest", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	// Location is like https://github.com/.../releases/tag/v1.2.3
	if i := strings.LastIndex(loc, "/"); i >= 0 {
		return loc[i+1:], nil
	}
	return "", fmt.Errorf("could not parse tag from redirect URL: %s", loc)
}

func assetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("ait-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}
