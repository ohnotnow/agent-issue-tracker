package ait

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const repoURL = "https://github.com/ohnotnow/agent-issue-tracker"

func CheckForUpdates(currentVersion string) error {
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

	return PrintJSON(map[string]any{
		"status":       "update_available",
		"old_version":  currentVersion,
		"new_version":  latestTag,
		"download_url": downloadURL,
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
