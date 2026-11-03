// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/config"
	"github.com/thousandeyes/thousandeyes-cli/internal/synthetics"
	"golang.org/x/text/unicode/norm"
)

func newSyntheticsCommand(getConfig func() config.Config) *cobra.Command {
	group := &cobra.Command{Use: "synthetics", Short: "Run synthetic tests as CI checks"}
	// Keep inherited authentication and output flags visible in Cobra help.
	group.SetHelpFunc(group.HelpFunc())
	var configPath, accountGroupID, jsonPath, junitPath string
	var timeout, pollInterval time.Duration
	var jsonOutput bool
	run := &cobra.Command{
		Use: "run-tests", Short: "Create instant tests, wait for results, and evaluate CI checks",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("timeout") && timeout <= 0 {
				return errors.New("--timeout must be positive")
			}
			if cmd.Flags().Changed("poll-interval") && pollInterval <= 0 {
				return errors.New("--poll-interval must be positive")
			}
			suite, err := synthetics.LoadConfig(configPath)
			if err != nil {
				return err
			}
			if err := validateReportPaths(configPath, jsonPath, junitPath); err != nil {
				return err
			}
			cfg := getConfig()
			report, runErr := synthetics.Run(cmd.Context(), suite, synthetics.Options{
				BaseURL: cfg.BaseURL, Token: cfg.Token, AccountGroupID: accountGroupID,
				Timeout: timeout, PollInterval: pollInterval,
			})
			var outputErrs []error
			if jsonOutput {
				outputErrs = append(outputErrs, synthetics.WriteJSON(cmd.OutOrStdout(), report))
			} else {
				outputErrs = append(outputErrs, synthetics.WriteSummary(cmd.OutOrStdout(), report))
			}
			if jsonPath != "" {
				outputErrs = append(outputErrs, writeReportFile(jsonPath, func(w io.Writer) error { return synthetics.WriteJSON(w, report) }))
			}
			if junitPath != "" {
				outputErrs = append(outputErrs, writeReportFile(junitPath, func(w io.Writer) error { return synthetics.WriteJUnit(w, report) }))
			}
			if outputErr := errors.Join(outputErrs...); outputErr != nil {
				return errors.Join(runErr, outputErr)
			}
			if runErr != nil {
				return runErr
			}
			if report.Failed() {
				return errors.New("synthetic CI checks failed; see the report for details")
			}
			return nil
		},
	}
	run.Flags().StringVar(&configPath, "config", "", "Path to the version 1 synthetic suite JSON file")
	_ = run.MarkFlagRequired("config")
	run.Flags().StringVar(&accountGroupID, "account-group-id", "", "Override the account group used for this execution")
	run.Flags().DurationVar(&timeout, "timeout", 0, "Override the suite timeout (for example 5m)")
	run.Flags().DurationVar(&pollInterval, "poll-interval", 0, "Override the result polling interval (for example 5s)")
	run.Flags().StringVar(&jsonPath, "json-report", "", "Write a sanitized JSON report to this file")
	run.Flags().StringVar(&junitPath, "junit-report", "", "Write a JUnit XML report to this file")
	group.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Print the sanitized CI report as JSON")
	group.AddCommand(run)
	return group
}

func validateReportPaths(configPath string, reportPaths ...string) error {
	configEntry, err := reportEntryPath(configPath)
	if err != nil {
		return err
	}
	configTarget, err := filepath.EvalSymlinks(configEntry)
	if err != nil {
		return err
	}
	// Reading follows a final configuration symlink; writing atomically replaces
	// a final report symlink. Protect both the configuration entry and its target.
	seen := []string{configEntry, configTarget}
	for _, path := range reportPaths {
		if path == "" {
			continue
		}
		entry, err := reportEntryPath(path)
		if err != nil {
			return err
		}
		for _, previous := range seen {
			same, err := sameReportEntry(previous, entry)
			if err != nil {
				return err
			}
			if same {
				return errors.New("configuration and report files must use different paths")
			}
		}
		seen = append(seen, entry)
	}
	return nil
}

func sameReportEntry(left, right string) (bool, error) {
	leftInfo, err := os.Lstat(left)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	rightInfo, err := os.Lstat(right)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if leftInfo != nil && rightInfo != nil && os.SameFile(leftInfo, rightInfo) {
		return true, nil
	}
	leftName := norm.NFC.String(filepath.Base(left))
	rightName := norm.NFC.String(filepath.Base(right))
	if !strings.EqualFold(leftName, rightName) {
		return false, nil
	}
	leftParent, err := os.Stat(filepath.Dir(left))
	if err != nil {
		return false, err
	}
	rightParent, err := os.Stat(filepath.Dir(right))
	if err != nil {
		return false, err
	}
	// Reserve case and canonical Unicode variants in the same directory on
	// every OS, even before outputs exist, so suites remain portable.
	return os.SameFile(leftParent, rightParent), nil
}

func reportEntryPath(path string) (string, error) {
	// Split before cleaning so a parent symlink followed by ".." retains its
	// filesystem meaning. The final entry is not followed because Rename replaces it.
	directory, name := filepath.Split(path)
	if directory == "" {
		directory = "."
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	// Abs may introduce a symlink through the working directory or macOS /var.
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

func writeReportFile(path string, write func(io.Writer) error) error {
	entry, err := reportEntryPath(path)
	if err != nil {
		return fmt.Errorf("resolve report %q: %w", path, err)
	}
	file, err := os.CreateTemp(filepath.Dir(entry), ".thousandeyes-report-*")
	if err != nil {
		return fmt.Errorf("create report %q: %w", path, err)
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err := write(file); err != nil {
		_ = file.Close()
		return fmt.Errorf("write report %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close report %q: %w", path, err)
	}
	if err := os.Rename(temp, entry); err != nil {
		return fmt.Errorf("save report %q: %w", path, err)
	}
	return nil
}
