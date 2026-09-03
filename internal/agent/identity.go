package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

var platformUUIDPattern = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

func ResolveID(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if runtime.GOOS != "darwin" {
		return "", errors.New("agent.id is required outside macOS")
	}
	output, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("read macOS platform UUID: %w", err)
	}
	match := platformUUIDPattern.FindSubmatch(output)
	if len(match) != 2 {
		return "", errors.New("macOS platform UUID was not present in ioreg output")
	}
	return deriveAgentID(string(match[1])), nil
}

func deriveAgentID(platformUUID string) string {
	normalized := strings.ToLower(strings.TrimSpace(platformUUID))
	digest := sha256.Sum256([]byte("orbit.agent.v1\x00" + normalized))
	return "agt_" + hex.EncodeToString(digest[:16])
}
