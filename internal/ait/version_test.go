package ait

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"v1.0.0", "v0.9.0", true},
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.1", false},
		{"v1.10.0", "v1.9.0", true},
		{"v1.9.0", "v1.10.0", false},
		{"invalid", "v1.0.0", false},
		{"v1.0.0", "invalid", false},
		{"", "", false},
		{"v1.0", "v1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.latest+"_vs_"+tt.current, func(t *testing.T) {
			got := isNewer(tt.latest, tt.current)
			if got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestBuildAPIURL(t *testing.T) {
	tests := []struct {
		repoURL string
		want    string
	}{
		{
			"https://github.com/ohnotnow/agent-issue-tracker",
			"https://api.github.com/repos/ohnotnow/agent-issue-tracker/releases/latest",
		},
		{
			"https://github.com/ohnotnow/agent-issue-tracker/",
			"https://api.github.com/repos/ohnotnow/agent-issue-tracker/releases/latest",
		},
		{
			"https://github.com/someuser/somefork",
			"https://api.github.com/repos/someuser/somefork/releases/latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.repoURL, func(t *testing.T) {
			got := buildAPIURL(tt.repoURL)
			if got != tt.want {
				t.Errorf("buildAPIURL(%q) = %q, want %q", tt.repoURL, got, tt.want)
			}
		})
	}
}
