package ait

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	Version = "dev"
	RepoURL = "https://github.com/ohnotnow/agent-issue-tracker"
)

// ghAsset is a single binary attached to a GitHub release.
type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// ghRelease is the slice of the GitHub /releases/latest payload that ait
// cares about — tag for version comparison, body for release notes, and
// assets so self-update can find the right binary.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

func RunVersion() error {
	fmt.Printf("ait version %s\n", Version)
	fmt.Println(skillsStatus(skillFiles, defaultClaudeDir()))

	if Version == "dev" {
		return nil
	}

	latest, err := checkLatestRelease()
	if err != nil {
		return nil
	}

	if isNewer(latest, Version) {
		fmt.Printf("A newer version (%s) is available.\n", latest)
		fmt.Printf("Visit %s/releases/latest to update, or run `ait self-update`.\n", RepoURL)
	} else {
		fmt.Println("You are running the latest version.")
	}

	return nil
}

// fetchLatestRelease asks the GitHub API for the latest published release
// at apiURL and returns the parsed payload. The caller supplies the
// http.Client so it can pick an appropriate timeout — `version` wants a
// short one (so a slow network does not make the command feel hung), while
// `self-update` needs longer for a multi-megabyte binary download.
func fetchLatestRelease(client *http.Client, apiURL string) (*ghRelease, error) {
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// checkLatestRelease is a thin wrapper around fetchLatestRelease that
// returns just the tag — the `version` command does not need anything else.
// Five-second timeout keeps an unreachable network from making the command
// feel hung.
func checkLatestRelease() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	rel, err := fetchLatestRelease(client, buildAPIURL(RepoURL))
	if err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func buildAPIURL(repoURL string) string {
	path := strings.TrimPrefix(repoURL, "https://github.com/")
	path = strings.TrimPrefix(path, "http://github.com/")
	path = strings.TrimSuffix(path, "/")
	return "https://api.github.com/repos/" + path + "/releases/latest"
}

func isNewer(latest, current string) bool {
	parse := func(v string) (int, int, int, bool) {
		v = strings.TrimPrefix(v, "v")
		parts := strings.Split(v, ".")
		if len(parts) != 3 {
			return 0, 0, 0, false
		}
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		patch, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, false
		}
		return major, minor, patch, true
	}

	lMaj, lMin, lPat, lok := parse(latest)
	cMaj, cMin, cPat, cok := parse(current)
	if !lok || !cok {
		return false
	}

	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}
