package ait

import "testing"

func TestParseEditorMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		title   string
		desc    string
		wantErr bool
	}{
		{
			name:  "title only",
			input: "Fix the login bug\n",
			title: "Fix the login bug",
			desc:  "",
		},
		{
			name:  "title and description",
			input: "Fix the login bug\n\nUsers were getting 500 errors on submit.\nThis was due to a nil session.",
			title: "Fix the login bug",
			desc:  "Users were getting 500 errors on submit.\nThis was due to a nil session.",
		},
		{
			name:  "comments stripped",
			input: "# Enter issue details.\nFix the login bug\n\nSome description\n# This is a comment",
			title: "Fix the login bug",
			desc:  "Some description",
		},
		{
			name:  "leading blank lines ignored",
			input: "\n\n  \nFix the login bug\n",
			title: "Fix the login bug",
			desc:  "",
		},
		{
			name:    "empty aborts",
			input:   "# just comments\n#\n",
			wantErr: true,
		},
		{
			name:    "all blank aborts",
			input:   "\n  \n\n",
			wantErr: true,
		},
		{
			name:  "template content parsed correctly",
			input: "\n# Enter issue details. First line is the title, everything after\n# a blank line is the description. Lines starting with # are ignored.\n# Save and close the editor to create the issue; leave it empty to abort.\nAdd caching layer\n\nRedis-based caching for API responses",
			title: "Add caching layer",
			desc:  "Redis-based caching for API responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, desc, err := parseEditorMessage(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if title != tt.title {
				t.Errorf("title = %q, want %q", title, tt.title)
			}
			if desc != tt.desc {
				t.Errorf("desc = %q, want %q", desc, tt.desc)
			}
		})
	}
}

// Opening an issue in the editor and saving it untouched must give back
// exactly what was there: Markdown headings, comments inside code blocks
// and titles starting with # are content, not instructions.
func TestEditorTemplateRoundTripsUnchanged(t *testing.T) {
	tests := []struct {
		name, title, desc string
	}{
		{"headings", "Fix the login bug", "## Acceptance criteria\n\nKeep the content."},
		{"fenced comment", "Document setup", "Run:\n\n```sh\n# install deps first\nmake\n```"},
		{"indented hash", "Tidy config", "Example:\n\n    # not a comment"},
		{"hash title", "#42 regression in export", "Body"},
		{"title only", "Just a title", ""},
		{"line straight after title", "Title", "No blank line before me"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, desc, err := parseEditorMessage(editorTemplate(tt.title, tt.desc))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if title != tt.title || desc != tt.desc {
				t.Fatalf("got (%q, %q), want (%q, %q)", title, desc, tt.title, tt.desc)
			}
		})
	}
}

func TestEditorCreateTemplateWithNothingEnteredAborts(t *testing.T) {
	if _, _, err := parseEditorMessage(editorTemplate("", "")); err == nil {
		t.Fatal("expected an untouched create template to abort")
	}
}
