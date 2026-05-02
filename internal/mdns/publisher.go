package mdns

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
)

var ErrBonjourUnavailable = errors.New("bonjour advertisement unavailable")

type Process interface {
	Kill() error
	Wait() error
}

type Runner interface {
	Start(name string, args ...string) (Process, error)
}

type Publisher interface {
	Start(ctx context.Context, desc Descriptor, port int) error
	Stop(ctx context.Context) error
}

type commandPublisher struct {
	runner Runner

	mu      sync.Mutex
	process Process
	started bool
}

type unavailablePublisher struct{}

type execProcess struct {
	cmd *exec.Cmd
}

func NewPublisher(runner Runner) Publisher {
	if runner == nil {
		runner = systemRunner{}
	}
	return &commandPublisher{runner: runner}
}

func (p *commandPublisher) Start(ctx context.Context, desc Descriptor, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if port <= 0 {
		return fmt.Errorf("listener port must be greater than zero")
	}
	if err := ValidateTXT(desc.TXTRecords()); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return nil
	}

	args := []string{"-R", desc.DeviceName, desc.ServiceType, "local", strconv.Itoa(port)}
	args = append(args, desc.TXTRecords()...)
	proc, err := p.runner.Start("dns-sd", args...)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBonjourUnavailable, err)
	}
	p.process = proc
	p.started = true
	return nil
}

func (p *commandPublisher) Stop(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	proc := p.process
	p.process = nil
	p.started = false
	p.mu.Unlock()

	if proc == nil {
		return nil
	}
	if err := proc.Kill(); err != nil {
		return err
	}
	return proc.Wait()
}

func (unavailablePublisher) Start(context.Context, Descriptor, int) error { return ErrBonjourUnavailable }
func (unavailablePublisher) Stop(context.Context) error                   { return nil }

func (r systemRunner) Start(name string, args ...string) (Process, error) {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd}, nil
}

func (p *execProcess) Kill() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *execProcess) Wait() error {
	if p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}
