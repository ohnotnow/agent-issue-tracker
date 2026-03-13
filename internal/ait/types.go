package ait

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
	StatusCancelled  = "cancelled"
)

type CLIError struct {
	Code     string
	Message  string
	ExitCode int
}

func (e *CLIError) Error() string {
	return e.Message
}

type Issue struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	ParentID    *string `json:"parent_id"`
	Priority    string  `json:"priority"`
	ClaimedBy   *string `json:"claimed_by"`
	ClaimedAt   *string `json:"claimed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	ClosedAt    *string `json:"closed_at"`
}

type Note struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type IssueRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Priority string `json:"priority"`
}

type ShowResponse struct {
	Issue    Issue      `json:"issue"`
	Children []Issue    `json:"children"`
	Blockers []IssueRef `json:"blockers"`
	Blocks   []IssueRef `json:"blocks"`
	Notes    []Note     `json:"notes"`
}

type DependencyTree struct {
	Issue    IssueRef         `json:"issue"`
	Blockers []DependencyTree `json:"blockers"`
	Cycles   []string         `json:"cycles,omitempty"`
}

func ValidateIssueType(value string) error {
	switch value {
	case "task", "epic", "initiative":
		return nil
	default:
		return &CLIError{Code: "validation", Message: "type must be one of: task, epic, initiative", ExitCode: 65}
	}
}

// ValidateParentType checks that the child type is compatible with the parent type.
// An empty parentType means no parent is being assigned, which is always valid.
func ValidateParentType(childType, parentType string) error {
	if parentType == "" {
		return nil
	}
	switch childType {
	case "initiative":
		return &CLIError{Code: "validation", Message: "initiatives cannot have a parent", ExitCode: 65}
	case "epic":
		if parentType != "initiative" {
			return &CLIError{Code: "validation", Message: "epics can only have an initiative as parent", ExitCode: 65}
		}
	case "task":
		if parentType != "epic" && parentType != "task" {
			return &CLIError{Code: "validation", Message: "tasks can only have an epic or task as parent", ExitCode: 65}
		}
	}
	return nil
}

func ValidateStatus(value string) error {
	switch value {
	case StatusOpen, StatusInProgress, StatusClosed, StatusCancelled:
		return nil
	default:
		return &CLIError{Code: "validation", Message: "status must be one of: open, in_progress, closed, cancelled", ExitCode: 65}
	}
}

func ValidatePriority(value string) error {
	switch value {
	case "P0", "P1", "P2", "P3", "P4":
		return nil
	default:
		return &CLIError{Code: "validation", Message: "priority must be one of: P0, P1, P2, P3, P4", ExitCode: 65}
	}
}

func ValidateTransition(from, to string) error {
	if from == to {
		return nil
	}

	allowed := map[string]map[string]bool{
		StatusOpen: {
			StatusInProgress: true,
			StatusClosed:     true,
			StatusCancelled:  true,
		},
		StatusInProgress: {
			StatusOpen:      true,
			StatusClosed:    true,
			StatusCancelled: true,
		},
		StatusClosed: {
			StatusOpen: true,
		},
		StatusCancelled: {
			StatusOpen: true,
		},
	}

	if allowed[from][to] {
		return nil
	}

	return &CLIError{
		Code:     "invalid_transition",
		Message:  fmt.Sprintf("cannot transition issue from %s to %s", from, to),
		ExitCode: 65,
	}
}

func NewID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func CommandNameForStatus(status string) string {
	switch status {
	case StatusClosed:
		return "close"
	case StatusCancelled:
		return "cancel"
	default:
		return "update"
	}
}

func PrintJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func NormalizeError(err error) *CLIError {
	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return cliErr
	}

	if errors.Is(err, sql.ErrNoRows) {
		return &CLIError{Code: "not_found", Message: "record not found", ExitCode: 66}
	}

	return &CLIError{
		Code:     "internal",
		Message:  err.Error(),
		ExitCode: 1,
	}
}

func ExitWithError(err *CLIError) {
	_ = PrintJSON(map[string]any{
		"error": map[string]any{
			"code":    err.Code,
			"message": err.Message,
		},
	})
	os.Exit(err.ExitCode)
}

func scanIssue(scanner interface{ Scan(dest ...any) error }) (Issue, error) {
	var iss Issue
	var parentID, claimedBy, claimedAt, closedAt sql.NullString

	err := scanner.Scan(
		&iss.ID,
		&iss.Type,
		&iss.Title,
		&iss.Description,
		&iss.Status,
		&parentID,
		&iss.Priority,
		&claimedBy,
		&claimedAt,
		&iss.CreatedAt,
		&iss.UpdatedAt,
		&closedAt,
	)
	if err != nil {
		return Issue{}, err
	}
	if parentID.Valid {
		iss.ParentID = &parentID.String
	}
	if claimedBy.Valid {
		iss.ClaimedBy = &claimedBy.String
	}
	if claimedAt.Valid {
		iss.ClaimedAt = &claimedAt.String
	}
	if closedAt.Valid {
		iss.ClosedAt = &closedAt.String
	}
	return iss, nil
}

func scanIssueRefs(rows *sql.Rows) ([]IssueRef, error) {
	items := make([]IssueRef, 0)
	for rows.Next() {
		var ref IssueRef
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Status, &ref.Type, &ref.Priority); err != nil {
			return nil, err
		}
		items = append(items, ref)
	}
	return items, rows.Err()
}
