package ait

import (
	"fmt"
	"strings"
	"sync"

	sqids "github.com/sqids/sqids-go"
)

const (
	publicIDPrefix    = "ait-"
	publicIDMinLength = 5
)

var (
	sqidsOnce  sync.Once
	sqidsErr   error
	sqidsCodec *sqids.Sqids
)

func issueKeyCodec() (*sqids.Sqids, error) {
	sqidsOnce.Do(func() {
		sqidsCodec, sqidsErr = sqids.New(sqids.Options{
			MinLength: publicIDMinLength,
		})
	})

	return sqidsCodec, sqidsErr
}

func PublicIDFromInternalID(id int64) (string, error) {
	if id < 0 {
		return "", fmt.Errorf("internal issue ids must be non-negative")
	}

	codec, err := issueKeyCodec()
	if err != nil {
		return "", err
	}

	encoded, err := codec.Encode([]uint64{uint64(id)})
	if err != nil {
		return "", err
	}

	return publicIDPrefix + encoded, nil
}

func CanonicalizePublicID(value string) (string, error) {
	if !strings.HasPrefix(value, publicIDPrefix) {
		return "", &CLIError{Code: "validation", Message: "issue id must start with " + publicIDPrefix, ExitCode: 65}
	}

	codec, err := issueKeyCodec()
	if err != nil {
		return "", err
	}

	raw := strings.TrimPrefix(value, publicIDPrefix)
	decoded := codec.Decode(raw)
	if len(decoded) != 1 {
		return "", &CLIError{Code: "validation", Message: fmt.Sprintf("issue id %s is not valid", value), ExitCode: 65}
	}

	reencoded, err := codec.Encode(decoded)
	if err != nil {
		return "", err
	}

	canonical := publicIDPrefix + reencoded
	if canonical != value {
		return "", &CLIError{Code: "validation", Message: fmt.Sprintf("issue id %s is not canonical; use %s", value, canonical), ExitCode: 65}
	}

	return canonical, nil
}
