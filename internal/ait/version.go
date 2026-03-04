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

func RunVersion() error {
	fmt.Printf("ait version %s\n", Version)

	if Version == "dev" {
		return nil
	}

	latest, err := checkLatestRelease()
	if err != nil {
		return nil
	}

	if isNewer(latest, Version) {
		fmt.Printf("A newer version (%s) is available.\n", latest)
		fmt.Printf("Visit %s/releases/latest to update.\n", RepoURL)
	} else {
		fmt.Println("You are running the latest version.")
	}

	return nil
}

func checkLatestRelease() (string, error) {
	apiURL := buildAPIURL(RepoURL)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.TagName, nil
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
