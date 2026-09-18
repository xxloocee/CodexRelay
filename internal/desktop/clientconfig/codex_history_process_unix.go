//go:build !windows

package clientconfig

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func codexHistoryProcessNames() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-A", "-o", "comm=").Output()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(output)) == "" {
		return nil, nil
	}
	return strings.Split(string(output), "\n"), nil
}
