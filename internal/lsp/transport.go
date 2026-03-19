package lsp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type Process interface {
	Wait() error
	Kill() error
}

type Transport interface {
	io.ReadWriteCloser
	Process() Process
}

type TransportFactory interface {
	Start(context.Context, LaunchSpec) (Transport, error)
}

type LaunchSpec struct {
	Command string
	Args    []string
	Env     map[string]string
	Dir     string
}

type CommandTransportFactory struct{}

func (CommandTransportFactory) Start(ctx context.Context, spec LaunchSpec) (Transport, error) {
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = os.Environ()
	for k, v := range spec.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command %q: %w", spec.Command, err)
	}

	return &stdioTransport{
		ReadCloser:  stdout,
		WriteCloser: stdin,
		cmd:         cmd,
	}, nil
}

type stdioTransport struct {
	io.ReadCloser
	io.WriteCloser
	cmd *exec.Cmd
}

func (s *stdioTransport) Close() error {
	_ = s.WriteCloser.Close()
	return s.ReadCloser.Close()
}

func (s *stdioTransport) Process() Process {
	return s.cmdProcess()
}

func (s *stdioTransport) cmdProcess() Process {
	return (*commandProcess)(s.cmd)
}

type commandProcess exec.Cmd

func (c *commandProcess) Wait() error {
	return (*exec.Cmd)(c).Wait()
}

func (c *commandProcess) Kill() error {
	if (*exec.Cmd)(c).Process == nil {
		return nil
	}
	return (*exec.Cmd)(c).Process.Kill()
}
