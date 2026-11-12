// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// generate-config: an unknown model format is invalid input, a file that cannot be created
// or a stdout that cannot be written is an output failure.
func TestGenerateConfigFailures(t *testing.T) {
	run(t, "server", "generate-config", "--format", "xml").want(t, 3).errHas(t, "invalid --format")
	missing := filepath.Join(t.TempDir(), "no", "such", "dir", "model.yaml")
	run(t, "server", "generate-config", "--output", missing).want(t, 10)
	if code := Run(context.Background(), []string{"server", "generate-config"}, failingWriter{}, &syncBuffer{}); code != 10 {
		t.Errorf("stdout write failure: exit %d, want 10", code)
	}
}
