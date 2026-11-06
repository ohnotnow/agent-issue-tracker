// Package aitfiles carries the Claude Code skills, agents and commands that go
// with this version of ait, built into the binary so `ait install-skills` can
// put them in place and `ait version` can tell when the installed copies
// differ. It lives at the repo root because go:embed only reaches files at or
// below the embedding package's directory.
package aitfiles

import "embed"

//go:embed claude
var Claude embed.FS
