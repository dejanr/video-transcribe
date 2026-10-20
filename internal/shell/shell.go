package shell

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Runner struct {
	Trace            bool
	Stdout           io.Writer
	KillProcessGroup bool
}

func (r Runner) stdout() io.Writer {
	if r.Stdout != nil {
		return r.Stdout
	}
	return os.Stdout
}

func (r Runner) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	if r.KillProcessGroup {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
	}
	return cmd
}

func (r Runner) Run(ctx context.Context, name string, args ...string) error {
	cmd := r.command(ctx, name, args...)
	cmd.Stdout = r.stdout()
	cmd.Stderr = os.Stderr
	startedAt := time.Now()
	r.traceCommand("start", cmd.Dir, name, args, startedAt, nil)
	if err := cmd.Run(); err != nil {
		r.traceCommand("error", cmd.Dir, name, args, startedAt, err)
		return fmt.Errorf("run %s %s: %w", name, strings.Join(args, " "), err)
	}
	r.traceCommand("done", cmd.Dir, name, args, startedAt, nil)
	return nil
}

func (r Runner) RunInDir(ctx context.Context, dir string, name string, args ...string) error {
	cmd := r.command(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = r.stdout()
	cmd.Stderr = os.Stderr
	startedAt := time.Now()
	r.traceCommand("start", cmd.Dir, name, args, startedAt, nil)
	if err := cmd.Run(); err != nil {
		r.traceCommand("error", cmd.Dir, name, args, startedAt, err)
		return fmt.Errorf("run in %s: %s %s: %w", dir, name, strings.Join(args, " "), err)
	}
	r.traceCommand("done", cmd.Dir, name, args, startedAt, nil)
	return nil
}

func (r Runner) Capture(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := r.command(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	startedAt := time.Now()
	r.traceCommand("start", cmd.Dir, name, args, startedAt, nil)
	if err := cmd.Run(); err != nil {
		r.traceCommand("error", cmd.Dir, name, args, startedAt, err)
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("run %s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return nil, fmt.Errorf("run %s %s: %w", name, strings.Join(args, " "), err)
	}
	r.traceCommand("done", cmd.Dir, name, args, startedAt, nil)
	return stdout.Bytes(), nil
}

func (r Runner) traceCommand(status string, dir string, name string, args []string, startedAt time.Time, err error) {
	if !r.Trace {
		return
	}
	cwd := dir
	if cwd == "" {
		cwd = "."
	}
	command := quotedCommand(name, args)
	elapsed := time.Since(startedAt).Round(time.Millisecond)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[trace] %s %s cwd=%s elapsed=%s err=%v\n", status, command, cwd, elapsed, err)
		return
	}
	fmt.Fprintf(os.Stderr, "[trace] %s %s cwd=%s elapsed=%s\n", status, command, cwd, elapsed)
}

func quotedCommand(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, strconv.Quote(name))
	for _, arg := range args {
		parts = append(parts, strconv.Quote(arg))
	}
	return strings.Join(parts, " ")
}

func NewRunDir(workspaceDir string) (string, error) {
	tmpRoot := filepath.Join(workspaceDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return "", fmt.Errorf("create tmp root: %w", err)
	}
	name, err := uniqueName()
	if err != nil {
		return "", err
	}
	runDir := filepath.Join(tmpRoot, name)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("create run tmp dir: %w", err)
	}
	return runDir, nil
}

func WriteJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json %s: %w", path, err)
	}
	return WriteText(path, string(append(body, '\n')))
}

func WriteText(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", path, err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file for %s: %w", path, err)
	}
	return nil
}

func PublishFile(src string, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %s: %w", src, err)
	}
	defer input.Close()

	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("stat source file %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", dst, err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", dst, err)
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, input); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", dst, err)
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file for %s: %w", dst, err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file for %s: %w", dst, err)
	}
	return nil
}

func uniqueName() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate unique suffix: %w", err)
	}
	return fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(suffix[:])), nil
}
