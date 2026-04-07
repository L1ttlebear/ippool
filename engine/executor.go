package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/L1ttlebear/ippool/database/auditlog"
	"github.com/L1ttlebear/ippool/database/models"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSSHConcurrency = 5
	sshConnectTimeout     = 30 * time.Second
	cmdExecTimeout        = 60 * time.Second
)

// ExecResult holds the result of a command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Error    error
}

// CommandExecutor executes pre-commands on hosts with concurrency control.
type CommandExecutor struct {
	sem chan struct{}
}

// NewCommandExecutor creates a CommandExecutor with the given max concurrency.
func NewCommandExecutor(maxConcurrency int) *CommandExecutor {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultSSHConcurrency
	}
	return &CommandExecutor{
		sem: make(chan struct{}, maxConcurrency),
	}
}

// Execute runs the host's PreCommand, respecting semaphore limits.
func (e *CommandExecutor) Execute(host models.Host) ExecResult {
	e.sem <- struct{}{}
	defer func() { <-e.sem }()

	start := time.Now()
	var result ExecResult

	if host.PreCommand == "" {
		result.Duration = time.Since(start)
		auditlog.EventLog("exec", fmt.Sprintf("host %d (%s): no pre-command configured", host.ID, host.Name))
		return result
	}

	// HTTP(S) URL - use GET request instead of SSH
	if strings.HasPrefix(host.PreCommand, "http://") || strings.HasPrefix(host.PreCommand, "https://") {
		result = e.execHTTP(host.PreCommand)
	} else {
		result = e.execSSH(host)
	}

	result.Duration = time.Since(start)

	auditlog.EventLog("exec", fmt.Sprintf(
		"host %d (%s): exit=%d duration=%s stdout=%q stderr=%q err=%v",
		host.ID, host.Name, result.ExitCode, result.Duration, result.Stdout, result.Stderr, result.Error,
	))

	return result
}

func (e *CommandExecutor) execHTTP(url string) ExecResult {
	ctx, cancel := context.WithTimeout(context.Background(), cmdExecTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ExecResult{ExitCode: 1, Error: err}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ExecResult{ExitCode: 1, Error: err}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	exitCode := 0
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		exitCode = 1
	}
	return ExecResult{
		Stdout:   string(body),
		ExitCode: exitCode,
	}
}

func (e *CommandExecutor) execSSH(host models.Host) ExecResult {
	ctx, cancel := context.WithTimeout(context.Background(), sshConnectTimeout)
	defer cancel()

	clientCh := make(chan *ssh.Client, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := dialSSH(host, sshConnectTimeout)
		if err != nil {
			errCh <- err
			return
		}
		clientCh <- c
	}()

	var client *ssh.Client
	select {
	case <-ctx.Done():
		return ExecResult{ExitCode: 1, Error: fmt.Errorf("SSH connect timeout")}
	case err := <-errCh:
		return ExecResult{ExitCode: 1, Error: err}
	case client = <-clientCh:
	}
	defer client.Close()

	execCtx, execCancel := context.WithTimeout(context.Background(), cmdExecTimeout)
	defer execCancel()

	session, err := client.NewSession()
	if err != nil {
		return ExecResult{ExitCode: 1, Error: fmt.Errorf("new session: %w", err)}
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- session.Run(host.PreCommand)
	}()

	select {
	case <-execCtx.Done():
		session.Signal(ssh.SIGKILL)
		return ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 1,
			Error:    fmt.Errorf("command execution timeout"),
		}
	case err = <-done:
	}

	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		}
	}

	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Error:    err,
	}
}
