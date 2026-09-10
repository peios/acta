//go:build unix

package providers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeCancelsDescendantsHoldingOutput(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("requires sh")
	}
	marker := filepath.Join(t.TempDir(), "orphan")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := (localRunner{}).Run(ctx, shell, "-c", `(sleep 0.5; echo survived > "$1") & wait`, "probe", marker)
	if !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatal(result.Err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("probe left a running descendant", err)
	}
}
