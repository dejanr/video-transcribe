package shell

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestRunnerCancelsDescendantsAndRedirectsOutput(t *testing.T) {
	var output bytes.Buffer
	runner := Runner{Stdout: &output, KillProcessGroup: true}
	if err := runner.Run(context.Background(), "/bin/sh", "-c", "printf ready"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "ready" {
		t.Fatal("output was not redirected")
	}
	output.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := runner.Run(ctx, "/bin/sh", "-c", "(sleep 0.3; printf orphan-survived) & wait")
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if output.Len() != 0 {
		t.Fatalf("a subprocess survived cancellation: %s", output.String())
	}
}
