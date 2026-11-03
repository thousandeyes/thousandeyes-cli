// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReportFileUsesResolvedParent(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		name := "success"
		if failWrite {
			name = "writer-error"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			logical, actual := filepath.Join(directory, "logical"), filepath.Join(directory, "actual")
			nested := filepath.Join(actual, "nested")
			for _, path := range []string{logical, nested} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(logical, "alias")
			symlinkSyntheticPath(t, nested, alias)
			if err := os.Chmod(logical, 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(logical, 0700) })
			actualInfo, err := os.Stat(actual)
			if err != nil {
				t.Fatal(err)
			}
			// Keep the symlink followed by ".." intact: its filesystem parent
			// is actual, whereas lexical cleaning would select logical.
			path := alias + string(filepath.Separator) + ".." + string(filepath.Separator) + "report.json"
			writeFailure := errors.New("intentional report writer failure")
			called := false
			err = writeReportFile(path, func(w io.Writer) error {
				called = true
				file, ok := w.(*os.File)
				if !ok {
					t.Fatalf("expected a temporary file writer, got %T", w)
				}
				tempParent, err := os.Stat(filepath.Dir(file.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(actualInfo, tempParent) {
					t.Errorf("temporary file was created outside the resolved destination directory")
				}
				if _, err := io.WriteString(w, "sanitized report\n"); err != nil {
					return err
				}
				if failWrite {
					return writeFailure
				}
				return nil
			})
			if !called {
				t.Fatal("report writer was not called for a writable resolved destination")
			}
			if failWrite {
				if !errors.Is(err, writeFailure) {
					t.Fatalf("report writer error was not preserved: %v", err)
				}
				if _, err := os.Stat(filepath.Join(actual, "report.json")); !os.IsNotExist(err) {
					t.Errorf("failed write created the destination: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(actual, "report.json"))
				if err != nil || string(data) != "sanitized report\n" {
					t.Fatalf("report missing from its resolved destination: %v", err)
				}
			}
			for _, parent := range []string{logical, actual} {
				entries, err := os.ReadDir(parent)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".thousandeyes-report-") {
						t.Errorf("temporary report %q was not cleaned up", entry.Name())
					}
				}
			}
		})
	}
}
