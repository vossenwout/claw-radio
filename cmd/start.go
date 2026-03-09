package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
	"github.com/vossenwout/claw-radio/internal/station"
)

const (
	clawPIDPattern      = "claw-radio-*.pid"
	mpvPIDFileName      = "claw-radio-mpv.pid"
	controllerPIDFile   = "claw-radio-controller.pid"
	ttsPIDFileName      = "claw-radio-tts.pid"
	mpvMissingMessage   = "mpv not found.\nInstall on macOS:  brew install mpv\nInstall on Linux:  apt install mpv   (Debian/Ubuntu)\n                   dnf install mpv   (Fedora)"
	ytdlpMissingMessage = "yt-dlp not found.\nInstall on macOS:  brew install yt-dlp\nInstall on Linux:  pip install yt-dlp"
	startReadyTimeout   = 2 * time.Minute
	startReadyPollEvery = 500 * time.Millisecond
)

type startReadinessClient interface {
	Close() error
	PlaylistCount() (int, error)
	Get(prop string) (json.RawMessage, error)
}

var (
	pidBaseDir = "/tmp"

	loadConfigFn               = config.Load
	startProcessFn             = defaultStartProcess
	waitForSocketFn            = waitForSocket
	executablePathFn           = os.Executable
	sendMPVQuitFn              = sendMPVQuit
	filepathGlobFn             = filepath.Glob
	removeFileFn               = os.Remove
	removeAllFn                = os.RemoveAll
	readFileFn                 = os.ReadFile
	writeFileFn                = os.WriteFile
	mkdirAllFn                 = os.MkdirAll
	openFileFn                 = os.OpenFile
	findProcessFn              = os.FindProcess
	statFileFn                 = os.Stat
	execCommandHelper          = exec.Command
	waitForStartReadyFn        = waitForStartReady
	dialStartReadinessClientFn = func(socketPath string) (startReadinessClient, error) {
		return mpv.Dial(socketPath)
	}
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the radio show",
	Long:  "Start your radio session and begin playback. If the radio is already running, this command keeps it as-is.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStart(cmd)
	},
}

func runStart(cmd *cobra.Command) error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if strings.TrimSpace(cfg.MPV.Binary) == "" {
		return exitCode(errors.New(mpvMissingMessage), 4)
	}
	if strings.TrimSpace(cfg.YtDlp.Binary) == "" {
		return exitCode(errors.New(ytdlpMissingMessage), 4)
	}

	mpvRunning := pidFileRunning(pidFilePath(mpvPIDFileName))
	controllerRunning := pidFileRunning(pidFilePath(controllerPIDFile))
	if mpvRunning && controllerRunning {
		fmt.Fprintln(cmd.OutOrStdout(), "radio is already running")
		return nil
	}

	_ = sendMPVQuitFn(cfg.MPV.Socket)

	if err := cleanupPIDFiles(); err != nil {
		return fmt.Errorf("cleanup stale pid files: %w", err)
	}

	if strings.TrimSpace(cfg.Station.StateDir) != "" {
		eventStore := station.NewAgentEventStore(cfg.Station.StateDir)
		if err := eventStore.ClearRuntimeState(); err != nil {
			return fmt.Errorf("reset runtime event state: %w", err)
		}
	}

	logFile, err := openLogFile(cfg.MPV.Log)
	if err != nil {
		return fmt.Errorf("open mpv log: %w", err)
	}
	defer logFile.Close()

	mpvArgs := []string{
		"--no-video",
		"--idle=yes",
		"--force-window=no",
		"--audio-display=no",
		"--cache=yes",
		"--cache-secs=20",
		"--demuxer-max-bytes=50MiB",
		fmt.Sprintf("--input-ipc-server=%s", cfg.MPV.Socket),
	}

	mpvProc, err := startProcessFn(cfg.MPV.Binary, mpvArgs, logFile, logFile)
	if err != nil {
		return fmt.Errorf("start mpv: %w", err)
	}

	mpvPIDPath := pidFilePath(mpvPIDFileName)
	if err := writePIDFile(mpvPIDPath, mpvProc.Pid); err != nil {
		_ = terminatePID(mpvProc.Pid)
		return fmt.Errorf("write mpv pid: %w", err)
	}

	if err := waitForSocketFn(cfg.MPV.Socket, 5*time.Second); err != nil {
		_ = terminatePID(mpvProc.Pid)
		_ = removeFileQuietly(mpvPIDPath)
		return exitCode(fmt.Errorf("wait for mpv socket: %w", err), 1)
	}

	exePath, err := executablePathFn()
	if err != nil {
		_ = terminatePID(mpvProc.Pid)
		_ = removeFileQuietly(mpvPIDPath)
		return fmt.Errorf("resolve current executable: %w", err)
	}

	controllerProc, err := startProcessFn(exePath, []string{"station", "daemon"}, logFile, logFile)
	if err != nil {
		_ = terminatePID(mpvProc.Pid)
		_ = removeFileQuietly(mpvPIDPath)
		return fmt.Errorf("start controller daemon: %w", err)
	}

	controllerPIDPath := pidFilePath(controllerPIDFile)
	if err := writePIDFile(controllerPIDPath, controllerProc.Pid); err != nil {
		_ = terminatePID(controllerProc.Pid)
		_ = terminatePID(mpvProc.Pid)
		_ = removeFileQuietly(controllerPIDPath)
		_ = removeFileQuietly(mpvPIDPath)
		return fmt.Errorf("write controller pid: %w", err)
	}

	if err := maybeStartTTSDaemon(cfg, logFile); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
	}

	if err := waitForStartReadyFn(cmd, cfg); err != nil {
		return exitCode(err, 1)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "radio started")
	return nil
}

func waitForStartReady(cmd *cobra.Command, cfg *config.Config) error {
	targetCount, shouldWait, err := startupReadyTarget(cfg)
	if err != nil {
		return err
	}
	if !shouldWait {
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "preparing your first song (this can take a while if it needs to be downloaded)...")

	deadline := time.Now().Add(startReadyTimeout)
	for time.Now().Before(deadline) {
		client, err := dialStartReadinessClientFn(cfg.MPV.Socket)
		if err == nil {
			count, countErr := client.PlaylistCount()
			idleActive, idleKnown := readStartBoolProperty(client, "idle-active")
			_ = client.Close()
			if countErr == nil && count >= targetCount {
				if !idleKnown || !idleActive {
					return nil
				}
			}
		}
		time.Sleep(startReadyPollEvery)
	}

	return fmt.Errorf("radio is running but still buffering the first song after %s; first-song downloads can take a while, check with: claw-radio status --json", startReadyTimeout)
}

func startupReadyTarget(cfg *config.Config) (int, bool, error) {
	if cfg == nil {
		return 0, false, nil
	}

	hasPendingIntro := false
	if strings.TrimSpace(cfg.Station.StateDir) != "" {
		pendingIntro, err := station.NewAgentEventStore(cfg.Station.StateDir).LoadPendingIntro()
		if err != nil {
			return 0, false, fmt.Errorf("load pending intro: %w", err)
		}
		hasPendingIntro = pendingIntro != nil && strings.TrimSpace(pendingIntro.AudioPath) != ""
	}

	st, err := station.Load(cfg.Station.StateDir)
	if err != nil {
		return 0, false, fmt.Errorf("load station state: %w", err)
	}
	hasSongs := len(st.Seeds) > 0

	if hasPendingIntro && !hasSongs {
		return 0, false, nil
	}

	if !hasPendingIntro && !hasSongs {
		return 0, false, nil
	}

	target := 0
	if hasPendingIntro {
		target++
	}
	if hasSongs {
		target++
	}
	if target == 0 {
		target = 1
	}

	return target, true, nil
}

func readStartBoolProperty(client startReadinessClient, prop string) (bool, bool) {
	raw, err := client.Get(prop)
	if err != nil {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func maybeStartTTSDaemon(cfg *config.Config, logFile *os.File) error {
	pythonPath := filepath.Join(cfg.TTS.DataDir, "venv", "bin", "python")
	if _, err := statFileFn(pythonPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat tts python binary: %w", err)
	}

	daemonPath := filepath.Join(cfg.TTS.DataDir, "daemon.py")
	if _, err := statFileFn(daemonPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat tts daemon script: %w", err)
	}

	ttsProc, err := startProcessFn(pythonPath, []string{daemonPath, cfg.TTS.Socket}, logFile, logFile)
	if err != nil {
		return fmt.Errorf("start tts daemon: %w", err)
	}

	if err := writePIDFile(pidFilePath(ttsPIDFileName), ttsProc.Pid); err != nil {
		_ = terminatePID(ttsProc.Pid)
		return fmt.Errorf("write tts pid: %w", err)
	}
	return nil
}

func cleanupPIDFiles() error {
	pidFiles, err := listPIDFiles()
	if err != nil {
		return err
	}

	for _, pidFile := range pidFiles {
		pid, err := readPIDFile(pidFile)
		if err == nil && pid > 0 {
			_ = terminatePID(pid)
		}
		if err := removeFileQuietly(pidFile); err != nil {
			return fmt.Errorf("remove pid file %s: %w", pidFile, err)
		}
	}
	return nil
}

func listPIDFiles() ([]string, error) {
	files, err := filepathGlobFn(filepath.Join(pidBaseDir, clawPIDPattern))
	if err != nil {
		return nil, fmt.Errorf("glob pid files: %w", err)
	}
	return files, nil
}

func readPIDFile(path string) (int, error) {
	data, err := readFileFn(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func pidFilePath(name string) string {
	return filepath.Join(pidBaseDir, name)
}

func writePIDFile(path string, pid int) error {
	return writeFileFn(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

func openLogFile(path string) (*os.File, error) {
	if err := mkdirAllFn(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return openFileFn(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

func removeFileQuietly(path string) error {
	err := removeFileFn(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func terminatePID(pid int) error {
	proc, err := findProcessFn(pid)
	if err != nil {
		return err
	}
	err = proc.Signal(syscall.SIGTERM)
	if isNoSuchProcessError(err) {
		return nil
	}
	return err
}

func isNoSuchProcessError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such process") || strings.Contains(msg, "already finished")
}

func sendMPVQuit(socketPath string) error {
	if strings.TrimSpace(socketPath) == "" {
		return nil
	}

	conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte("{\"command\":[\"quit\"]}\n"))
	return err
}

func waitForSocket(socketPath string, timeout time.Duration) error {
	return mpv.WaitForSocket(socketPath, timeout)
}

func defaultStartProcess(path string, args []string, stdout, stderr *os.File) (*os.Process, error) {
	cmd := execCommandHelper(path, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

func init() {
	RootCmd.AddCommand(startCmd)
}
