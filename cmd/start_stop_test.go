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

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/station"
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

func TestStartWhenAlreadyRunningPrintsNoOpMessage(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	mpvProc := spawnSleepProcess(t)
	controllerProc := spawnSleepProcess(t)
	if err := os.WriteFile(filepath.Join(tmp, mpvPIDFileName), []byte(fmt.Sprintf("%d\n", mpvProc.Pid)), 0o644); err != nil {
		t.Fatalf("write mpv pid: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, controllerPIDFile), []byte(fmt.Sprintf("%d\n", controllerProc.Pid)), 0o644); err != nil {
		t.Fatalf("write controller pid: %v", err)
	}

	restore := withEngineTestHooks(cfg, tmp)
	defer restore()

	called := false
	startProcessFn = func(string, []string, *os.File, *os.File) (*os.Process, error) {
		called = true
		return nil, fmt.Errorf("should not start process when already running")
	}

	err, stdout, _ := executeCommandWithOutputForTest("start")
	if err != nil {
		t.Fatalf("start command failed: %v", err)
	}
	if called {
		t.Fatal("start process was called for already-running radio")
	}
	if !strings.Contains(stdout, "already running") {
		t.Fatalf("stdout = %q, want already-running message", stdout)
	}
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

func TestStopWhenAlreadyStoppedPrintsNoOpMessage(t *testing.T) {
	tmp := t.TempDir()
	cfg := baseEngineTestConfig(tmp)

	restore := withEngineTestHooks(cfg, tmp)
	defer restore()

	err, stdout, _ := executeCommandWithOutputForTest("stop")
	if err != nil {
		t.Fatalf("stop command failed: %v", err)
	}
	if !strings.Contains(stdout, "already stopped") {
		t.Fatalf("stdout = %q, want already-stopped message", stdout)
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
	origWaitReady := waitForStartReadyFn

	loadConfigFn = func() (*config.Config, error) {
		copy := *cfg
		return &copy, nil
	}
	startProcessFn = defaultStartProcess
	waitForSocketFn = waitForSocket
	executablePathFn = func() (string, error) { return "/test/self", nil }
	pidBaseDir = pidDir
	waitForStartReadyFn = func(*cobra.Command, *config.Config) error { return nil }

	return func() {
		loadConfigFn = origLoad
		startProcessFn = origStart
		waitForSocketFn = origWait
		executablePathFn = origExe
		pidBaseDir = origPID
		waitForStartReadyFn = origWaitReady
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

func TestStartupReadyTargetSkipsWaitForIntroOnlyStart(t *testing.T) {
	stateDir := t.TempDir()
	store := station.NewAgentEventStore(stateDir)
	if err := store.SavePendingIntro(filepath.Join(stateDir, "intro.aiff")); err != nil {
		t.Fatalf("save pending intro: %v", err)
	}

	cfg := &config.Config{Station: config.StationConfig{StateDir: stateDir}}
	target, shouldWait, err := startupReadyTarget(cfg)
	if err != nil {
		t.Fatalf("startupReadyTarget failed: %v", err)
	}
	if target != 0 {
		t.Fatalf("target = %d, want 0", target)
	}
	if shouldWait {
		t.Fatal("shouldWait = true, want false for intro-only startup")
	}
}
