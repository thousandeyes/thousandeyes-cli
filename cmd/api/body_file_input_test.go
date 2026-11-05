// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestRequestBodyFileOpeningRespectsContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named FIFO fixture requires Unix")
	}
	path := filepath.Join(t.TempDir(), "request.json")
	if output, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
		t.Fatalf("create FIFO: %v\n%s", err, output)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.Flags().String("body-file", "", "")
	if err := command.Flags().Set("body-file", path); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := buildAPIRequestBodyWithFieldOverrides(command, nil, nil)
		finished <- err
	}()
	// The command has a ready context, but the FIFO has no writer.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("body file opening ignored cancellation")
	}
	// Unblock any outstanding OS open after cancellation; the helper owns
	// closing its reader even when the writer arrives late.
	writer, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
