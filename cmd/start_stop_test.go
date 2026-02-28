package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
)

func TestStartFailsWhenMPVMissing(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)
	cfg.MPV.Binary = ""
	cfg.YtDlp.Binary = "/usr/bin/yt-dlp"

	restore := withEngineTestHooks(cfg, tmp)
	defer restore()

	err := executeCommandForTest(t, "start")
	assertExitCode(t, err, 4)
	if !strings.Contains(err.Error(), "brew install mpv") {
		t.Fatalf("error must contain macOS install hint, got %q", err)
	}
	if !strings.Contains(err.Error(), "apt install mpv") {
		t.Fatalf("error must contain Linux install hint, got %q", err)
	}
}

func TestStartFailsWhenYtDlpMissing(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)
	cfg.MPV.Binary = "/usr/bin/mpv"
	cfg.YtDlp.Binary = ""

	restore := withEngineTestHooks(cfg, tmp)
	defer restore()

	err := executeCommandForTest(t, "start")
	assertExitCode(t, err, 4)
	if !strings.Contains(strings.ToLower(err.Error()), "yt-dlp") {
		t.Fatalf("error must mention yt-dlp, got %q", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "install") {
		t.Fatalf("error must contain install instructions, got %q", err)
	}
}

func TestStartReplacesStalePIDFiles(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	stale := spawnSleepProcess(t)
	stalePID := stale.Pid
	if err := os.WriteFile(filepath.Join(tmp, "claw-radio-mpv.pid"), []byte(fmt.Sprintf("%d\n", stalePID)), 0o644); err != nil {
		t.Fatalf("write stale pid: %v", err)
	}

	h := newProcessHarness(t)
	restore := withEngineTestHooks(cfg, tmp)
	defer restore()
	startProcessFn = h.start
	waitForSocketFn = func(string, time.Duration) error { return nil }

	if err := executeCommandForTest(t, "start"); err != nil {
		t.Fatalf("start command failed: %v", err)
	}

	newMPVPID := readPIDFromFile(t, filepath.Join(tmp, "claw-radio-mpv.pid"))
	if newMPVPID == stalePID {
		t.Fatalf("stale PID was not replaced: %d", stalePID)
	}
	if !processAlive(newMPVPID) {
		t.Fatalf("new mpv PID %d is not alive", newMPVPID)
	}

	if err := waitForProcessExit(stale, 2*time.Second); err != nil {
		t.Fatalf("stale process was not terminated: %v", err)
	}

	h.cleanup()
}

func TestStartWritesPIDFilesOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	h := newProcessHarness(t)
	restore := withEngineTestHooks(cfg, tmp)
	defer restore()
	startProcessFn = h.start
	waitForSocketFn = func(string, time.Duration) error { return nil }

	if err := executeCommandForTest(t, "start"); err != nil {
		t.Fatalf("start command failed: %v", err)
	}

	mpvPID := readPIDFromFile(t, filepath.Join(tmp, "claw-radio-mpv.pid"))
	controllerPID := readPIDFromFile(t, filepath.Join(tmp, "claw-radio-controller.pid"))
	if mpvPID <= 0 || controllerPID <= 0 {
		t.Fatalf("invalid pid values mpv=%d controller=%d", mpvPID, controllerPID)
	}
	if !processAlive(mpvPID) || !processAlive(controllerPID) {
		t.Fatalf("expected both started processes to be alive")
	}

	h.cleanup()
}

func TestStopAfterStartRemovesPIDsAndTerminatesProcesses(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	h := newProcessHarness(t)
	restore := withEngineTestHooks(cfg, tmp)
	defer restore()
	startProcessFn = h.start
	waitForSocketFn = func(string, time.Duration) error { return nil }

	if err := executeCommandForTest(t, "start"); err != nil {
		t.Fatalf("start command failed: %v", err)
	}

	mpvPID := readPIDFromFile(t, filepath.Join(tmp, "claw-radio-mpv.pid"))
	controllerPID := readPIDFromFile(t, filepath.Join(tmp, "claw-radio-controller.pid"))

	if err := executeCommandForTest(t, "stop"); err != nil {
		t.Fatalf("stop command failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "claw-radio-mpv.pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mpv pid file should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "claw-radio-controller.pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("controller pid file should be removed, err=%v", err)
	}
	if err := h.waitForPIDExit(mpvPID, 2*time.Second); err != nil {
		t.Fatalf("mpv process not terminated: %v", err)
	}
	if err := h.waitForPIDExit(controllerPID, 2*time.Second); err != nil {
		t.Fatalf("controller process not terminated: %v", err)
	}
}

func TestStopWithoutPIDFilesSucceeds(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	restore := withEngineTestHooks(cfg, tmp)
	defer restore()

	if err := executeCommandForTest(t, "stop"); err != nil {
		t.Fatalf("stop command with no pid files failed: %v", err)
	}
}

func TestStartReturnsExitCodeOneOnSocketTimeout(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	h := newProcessHarness(t)
	restore := withEngineTestHooks(cfg, tmp)
	defer restore()
	startProcessFn = h.start
	waitForSocketFn = func(string, time.Duration) error {
		return fmt.Errorf("timeout waiting for socket")
	}

	err := executeCommandForTest(t, "start")
	assertExitCode(t, err, 1)
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("timeout error expected, got %q", err)
	}

	h.cleanup()
}

func executeCommandForTest(t *testing.T, args ...string) error {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	RootCmd.SetArgs(args)
	return Execute()
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with exit code %d, got nil", want)
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("error type = %T, want *exitError", err)
	}
	if ee.code != want {
		t.Fatalf("exit code = %d, want %d", ee.code, want)
	}
}

type processHarness struct {
	procs []*os.Process
}

func newProcessHarness(t *testing.T) *processHarness {
	t.Helper()
	h := &processHarness{}
	t.Cleanup(h.cleanup)
	return h
}

func (h *processHarness) start(string, []string, *os.File, *os.File) (*os.Process, error) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	h.procs = append(h.procs, cmd.Process)
	return cmd.Process, nil
}

func (h *processHarness) cleanup() {
	for _, p := range h.procs {
		if p == nil {
			continue
		}
		_ = p.Signal(syscall.SIGTERM)
		_, _ = p.Wait()
	}
}

func (h *processHarness) waitForPIDExit(pid int, timeout time.Duration) error {
	for _, p := range h.procs {
		if p != nil && p.Pid == pid {
			return waitForProcessExit(p, timeout)
		}
	}
	return fmt.Errorf("pid %d not found in harness", pid)
}

func withEngineTestHooks(cfg *config.Config, pidDir string) func() {
	origLoad := loadConfigFn
	origStart := startProcessFn
	origWait := waitForSocketFn
	origExe := executablePathFn
	origPID := pidBaseDir

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	startProcessFn = defaultStartProcess
	waitForSocketFn = waitForSocket
	executablePathFn = func() (string, error) { return "/test/self", nil }
	pidBaseDir = pidDir

	return func() {
		loadConfigFn = origLoad
		startProcessFn = origStart
		waitForSocketFn = origWait
		executablePathFn = origExe
		pidBaseDir = origPID
	}
}

func baseEngineTestConfig(tmp string) *config.Config {
	return &config.Config{
		MPV: config.MPVConfig{
			Binary: "/usr/bin/mpv",
			Socket: filepath.Join(tmp, "mpv.sock"),
			Log:    filepath.Join(tmp, "logs", "mpv.log"),
		},
		YtDlp: config.BinaryConfig{Binary: "/usr/bin/yt-dlp"},
		TTS: config.TTSConfig{
			DataDir: filepath.Join(tmp, "tts"),
			Socket:  filepath.Join(tmp, "tts.sock"),
		},
	}
}

func readPIDFromFile(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse pid file %s: %v", path, err)
	}
	return pid
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

func waitForProcessExit(p *os.Process, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		_, err := p.Wait()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no child processes") {
			return err
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("pid %d still alive after %s", p.Pid, timeout)
	}
}

func spawnSleepProcess(t *testing.T) *os.Process {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process
}
