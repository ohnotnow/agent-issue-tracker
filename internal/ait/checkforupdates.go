package ait

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const repoURL = "https://github.com/ohnotnow/agent-issue-tracker"

// VersionString returns the version and, for release builds, checks if a
// newer version is available on GitHub. If the check fails it silently
// returns just the current version.
func VersionString(currentVersion string) string {
	if currentVersion == "dev" {
		return currentVersion
	}

	latestTag, err := fetchLatestTag()
	if err != nil || latestTag == currentVersion {
		return currentVersion
	}

	asset := assetName()
	downloadURL := fmt.Sprintf("%s/releases/download/%s/%s", repoURL, latestTag, asset)

	return fmt.Sprintf("%s - newer version %s available at %s", currentVersion, latestTag, downloadURL)
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
