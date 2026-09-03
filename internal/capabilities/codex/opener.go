package codex

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Opener struct{}

func NewOpener() *Opener {
	return &Opener{}
}

func ThreadURL(sessionID string) (string, error) {
	if !sessionIDPattern.MatchString(sessionID) {
		return "", errors.New("Codex session ID must be a lowercase UUID")
	}
	return "codex://threads/" + sessionID, nil
}

func (*Opener) Open(ctx context.Context, sessionID string) error {
	url, err := ThreadURL(sessionID)
	if err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, "/usr/bin/open", url).Run(); err != nil {
		return fmt.Errorf("open Codex session: %w", err)
	}
	return nil
}
