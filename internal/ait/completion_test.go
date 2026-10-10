package ait

import (
	"os/exec"
	"strings"
	"testing"
)

// macOS ships bash 3.2, which rejects the bash 4 ";;&" case terminator, so
// the whole completion script failed to load there.
func TestBashCompletionParsesInOldBash(t *testing.T) {
	script := generateBashCompletion()
	if strings.Contains(script, ";;&") {
		t.Fatal(`bash completion uses ";;&", which bash 3.2 cannot parse`)
	}

	// /bin/bash is 3.2 on macOS, so this is the real check there.
	cmd := exec.Command("/bin/bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("/bin/bash -n rejected the script: %v\n%s", err, out)
	}
}
