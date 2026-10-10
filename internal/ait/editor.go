package ait

import (
	"os"
	"os/exec"
	"slices"
	"strings"
)

// editorScissors separates the issue from the help text, as in git commit
// --cleanup=scissors: everything from this line down is ignored, so lines
// above it starting with # (Markdown headings, shell comments) are kept.
const editorScissors = "# ------------------------ >8 ------------------------"

const editorCreateHelp = editorScissors + `
# Enter the title on the first line and the description after it.
# Do not change or remove the line above; everything below it is ignored.
# Save and close the editor to create the issue; leave it empty to abort.
`

const editorUpdateHelp = editorScissors + `
# Edit the title on the first line and the description after it.
# Do not change or remove the line above; everything below it is ignored.
# Save and close the editor to apply the changes; clear the title to abort.
`

// EditIssueMessage opens $EDITOR (falling back to vi) with a template and
// parses the result into a title and description, similar to git commit.
// If initialTitle/initialDescription are non-empty they are pre-populated for
// the user to edit.
func EditIssueMessage(initialTitle, initialDescription string) (title, description string, err error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmp, err := os.CreateTemp("", "ait-edit-*.md")
	if err != nil {
		return "", "", &CLIError{Code: "io", Message: "cannot create temp file: " + err.Error(), ExitCode: 73}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(editorTemplate(initialTitle, initialDescription)); err != nil {
		tmp.Close()
		return "", "", &CLIError{Code: "io", Message: "cannot write temp file: " + err.Error(), ExitCode: 73}
	}
	tmp.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", "", &CLIError{Code: "io", Message: "editor exited with error: " + err.Error(), ExitCode: 1}
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", "", &CLIError{Code: "io", Message: "cannot read temp file: " + err.Error(), ExitCode: 73}
	}

	return parseEditorMessage(string(data))
}

// editorTemplate is the file the editor opens: the issue's title and
// description (empty when creating) followed by the help text.
func editorTemplate(title, description string) string {
	var body strings.Builder
	if title == "" && description == "" {
		body.WriteString("\n")
		body.WriteString(editorCreateHelp)
		return body.String()
	}
	body.WriteString(title)
	body.WriteString("\n\n")
	body.WriteString(description)
	if description != "" {
		body.WriteString("\n")
	}
	body.WriteString("\n")
	body.WriteString(editorUpdateHelp)
	return body.String()
}

// parseEditorMessage extracts a title and description from editor output.
// Everything from the scissors line down is dropped. The first non-blank
// line is the title and the rest, trimmed, is the description. If the
// scissors line was removed, every line starting with # is dropped instead,
// as before it existed, so the help text never lands in the description.
func parseEditorMessage(raw string) (title, description string, err error) {
	lines := strings.Split(raw, "\n")
	if cut := slices.IndexFunc(lines, func(line string) bool {
		return strings.TrimSpace(line) == editorScissors
	}); cut >= 0 {
		lines = lines[:cut]
	} else {
		lines = slices.DeleteFunc(lines, func(line string) bool {
			return strings.HasPrefix(strings.TrimSpace(line), "#")
		})
	}

	for i, line := range lines {
		if title = strings.TrimSpace(line); title != "" {
			description = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			break
		}
	}
	if title == "" {
		return "", "", &CLIError{Code: "validation", Message: "aborted: empty title", ExitCode: 1}
	}
	return title, description, nil
}
