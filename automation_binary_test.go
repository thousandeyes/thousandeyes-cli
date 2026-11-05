// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestAutomationBinaryContract(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "thousandeyes")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	var mode, requestBody atomic.Value
	mode.Store("success")
	requestBody.Store("")
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		io.WriteString(w, `{"redirected":true}`)
	}))
	defer destination.Close()
	started, interrupted := make(chan struct{}, 1), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer binary-fake-token" {
			t.Error("CLI did not use the injected token")
		}
		body, _ := io.ReadAll(request.Body)
		requestBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		switch value := mode.Load().(string); value {
		case "404", "429":
			w.Header().Set("Retry-After", "1")
			w.Header().Set("X-Organization-Rate-Limit-Reset", "1793000000")
			status := 404
			if value == "429" {
				status = 429
			}
			w.WriteHeader(status)
			io.WriteString(w, `{"private":"native-private-response","counter":9007199254740993}`)
		case "redirect":
			w.Header().Set("Location", destination.URL)
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "size":
			io.WriteString(w, strings.Repeat("x", 128))
		case "block":
			started <- struct{}{}
			<-request.Context().Done()
			interrupted <- struct{}{}
		default:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"testId":"101","counter":9007199254740993}`)
		}
	}))
	defer server.Close()
	command := func(args []string, input string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
		child := exec.Command(binary, args...)
		child.Env = automationEnvironment(server.URL)
		child.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		child.Stdout, child.Stderr = &stdout, &stderr
		return child, &stdout, &stderr
	}
	readEnvelope := func(t *testing.T, output string) (int, http.Header, json.RawMessage) {
		t.Helper()
		var envelope struct {
			StatusCode int             `json:"statusCode"`
			Headers    http.Header     `json:"headers"`
			Body       json.RawMessage `json:"body"`
		}
		decoder := json.NewDecoder(strings.NewReader(output))
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatalf("invalid envelope %q: %v", output, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("expected exactly one envelope: %q", output)
		}
		return envelope.StatusCode, envelope.Headers, envelope.Body
	}
	t.Run("stdin complete network body", func(t *testing.T) {
		const payload = `{"server":"target.invalid","port":443,"protocol":"tcp","agents":[{"agentId":"6"}],"fixedPacketRate":25,"numPathTraces":3,"probeMode":"syn","headers":"private-request-secret","future":9007199254740993}`
		args := []string{"tests", "agent-to-server", "instant", "--body-file", "-", "--aid", "123", "--expand", "agent", "--http-response", "--no-redirects", "--max-response-bytes", "4194304"}
		child, out, stderr := command(args, payload)
		if strings.Contains(strings.Join(child.Args, " "), "private-request-secret") {
			t.Fatal("stdin secret leaked into argv")
		}
		if err := child.Run(); err != nil {
			t.Fatalf("CLI failed: %v stderr=%s", err, stderr)
		}
		status, _, body := readEnvelope(t, out.String())
		if status != 201 || !bytes.Contains(body, []byte("9007199254740993")) || requestBody.Load().(string) != payload {
			t.Fatalf("request or response changed: body=%s request=%s", body, requestBody.Load())
		}
	})
	t.Run("generated network flags", func(t *testing.T) {
		child, _, stderr := command([]string{"tests", "agent-to-server", "instant", "--server", "target.invalid", "--protocol", "tcp", "--agents", `[{"agentId":"6"}]`, "--num-path-traces", "3", "--probe-mode", "syn"}, "")
		if err := child.Run(); err != nil {
			t.Fatalf("network flags failed: %v stderr=%s", err, stderr)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal([]byte(requestBody.Load().(string)), &body); err != nil || string(body["server"]) != `"target.invalid"` || string(body["numPathTraces"]) != "3" {
			t.Fatalf("network flags lost: %s", requestBody.Load())
		}
	})
	t.Run("body file preserves JSON", func(t *testing.T) {
		const payload = " {\n\"url\":\"https://target.invalid/private-file-secret\",\"agents\":[{\"agentId\":\"6\"}],\"counter\":9007199254740993}\n"
		path := filepath.Join(t.TempDir(), "request body.json")
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		child, _, stderr := command([]string{"tests", "http-server", "instant", "--body-file", path, "--http-response"}, "")
		if err := child.Run(); err != nil || requestBody.Load().(string) != payload {
			t.Fatalf("body file changed: error=%v stderr=%s", err, stderr)
		}
	})
	t.Run("FIFO body preserves JSON", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("named FIFO fixture requires Unix")
		}
		path := filepath.Join(t.TempDir(), "request.json")
		if output, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
			t.Fatalf("create FIFO: %v\n%s", err, output)
		}
		const payload = `{"url":"https://target.invalid","agents":[{"agentId":"6"}],"counter":9007199254740993}`
		written := make(chan error, 1)
		go func() {
			file, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err == nil {
				_, err = io.WriteString(file, payload)
				if closeErr := file.Close(); err == nil {
					err = closeErr
				}
			}
			written <- err
		}()
		child, _, stderr := command([]string{"tests", "http-server", "instant", "--body-file", path, "--http-response"}, "")
		if err := child.Run(); err != nil {
			t.Fatalf("FIFO request failed: %v stderr=%s", err, stderr)
		}
		if err := <-written; err != nil || requestBody.Load().(string) != payload {
			t.Fatalf("FIFO body changed: error=%v request=%s", err, requestBody.Load())
		}
	})
	for _, value := range []string{"404", "429"} {
		t.Run("HTTP "+value, func(t *testing.T) {
			mode.Store(value)
			child, out, stderr := command([]string{"tests", "list", "--http-response"}, "")
			if err := child.Run(); err == nil {
				t.Fatal("HTTP error exited zero")
			}
			status, headers, body := readEnvelope(t, out.String())
			if fmt.Sprint(status) != value || headers.Get("Retry-After") != "1" || headers.Get("X-Organization-Rate-Limit-Reset") != "1793000000" || !bytes.Contains(body, []byte("9007199254740993")) {
				t.Fatalf("HTTP error contract lost: %s", out)
			}
			if strings.Contains(stderr.String(), "native-private-response") {
				t.Fatal("raw HTTP error body echoed on stderr")
			}
		})
	}
	t.Run("redirect policy", func(t *testing.T) {
		mode.Store("redirect")
		child, out, _ := command([]string{"tests", "list", "--http-response", "--no-redirects"}, "")
		if err := child.Run(); err == nil {
			t.Fatal("unfollowed redirect must fail")
		}
		status, _, _ := readEnvelope(t, out.String())
		if status != 307 || redirected.Load() != 0 {
			t.Fatalf("redirect followed: status=%d calls=%d", status, redirected.Load())
		}
		child, _, stderr := command([]string{"tests", "list", "--json"}, "")
		if err := child.Run(); err != nil || redirected.Load() != 1 {
			t.Fatalf("default redirect changed: error=%v stderr=%s", err, stderr)
		}
	})
	t.Run("body allocation limit", func(t *testing.T) {
		mode.Store("size")
		child, out, _ := command([]string{"tests", "list", "--http-response", "--max-response-bytes", "127"}, "")
		if err := child.Run(); err == nil || out.Len() != 0 {
			t.Fatalf("oversized body accepted: error=%v stdout=%s", err, out)
		}
		child, out, stderr := command([]string{"tests", "list", "--http-response", "--max-response-bytes", "128"}, "")
		if err := child.Run(); err != nil {
			t.Fatalf("exact limit rejected: %v stderr=%s", err, stderr)
		}
		_, _, body := readEnvelope(t, out.String())
		var text string
		if err := json.Unmarshal(body, &text); err != nil || len(text) != 128 {
			t.Fatalf("text response lost: %s", body)
		}
	})
	t.Run("argument error exit", func(t *testing.T) {
		child, out, _ := command([]string{"--unknown-automation-flag"}, "")
		if err := child.Run(); err == nil || out.Len() != 0 {
			t.Fatal("argument error must exit nonzero without an envelope")
		}
	})
	t.Run("signal interrupts body file opening", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("named pipes and SIGTERM delivery require Unix")
		}
		path := filepath.Join(t.TempDir(), "request.json")
		if output, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
			t.Fatalf("create FIFO: %v\n%s", err, output)
		}
		child, out, stderr := command([]string{"tests", "http-server", "instant", "--body-file", path, "--http-response"}, "")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer child.Process.Kill()
		// Registration precedes main; wait for the installed signal handler
		// before interrupting the FIFO open, whose writer never appears.
		time.Sleep(5 * time.Second)
		if err := child.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { finished <- child.Wait() }()
		select {
		case err := <-finished:
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "context canceled") {
				t.Fatalf("expected handled cancellation with exit 1 and no envelope: error=%v stdout=%s stderr=%s", err, out, stderr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("CLI remained blocked opening a FIFO after SIGTERM")
		}
	})
	t.Run("signal interrupts body input", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("os.Interrupt delivery is unavailable on Windows")
		}
		child, out, _ := command([]string{"tests", "http-server", "instant", "--body-file", "-", "--http-response"}, "")
		child.Stdin = nil
		input, err := child.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer child.Process.Kill()
		if _, err := io.WriteString(input, "{"); err != nil {
			t.Fatal(err)
		}
		// Command registration runs before main; allow it to finish so this
		// exercises the installed signal handler while stdin remains open.
		time.Sleep(3 * time.Second)
		if err := child.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { finished <- child.Wait() }()
		select {
		case err := <-finished:
			if err == nil || out.Len() != 0 {
				t.Fatal("interrupted body input must fail without an envelope")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("CLI remained blocked on stdin after interruption")
		}
	})
	t.Run("signal interrupts active request", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("os.Interrupt delivery is unavailable on Windows")
		}
		mode.Store("block")
		child, out, stderr := command([]string{"tests", "list", "--http-response"}, "")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer child.Process.Kill()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("request did not start")
		}
		if err := child.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { finished <- child.Wait() }()
		select {
		case err := <-finished:
			if err == nil || out.Len() != 0 {
				t.Fatalf("cancellation passed: error=%v stdout=%s stderr=%s", err, out, stderr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("CLI did not exit after interruption")
		}
		select {
		case <-interrupted:
		case <-time.After(time.Second):
			t.Fatal("request context was not canceled")
		}
	})
}

func automationEnvironment(base string) []string {
	var environment []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key != "TE_TOKEN" && key != "TE_BASE_URL" {
			environment = append(environment, item)
		}
	}
	return append(environment, "TE_TOKEN=binary-fake-token", "TE_BASE_URL="+base)
}
