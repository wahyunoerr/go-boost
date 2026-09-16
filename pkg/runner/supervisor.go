package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wahyunoerr/go-boost/v2/pkg/diagnostics"
)

const maxSupervisedLine = 1 << 20

type SuperviseOptions struct {
	Stdout io.Writer
	Stderr io.Writer

	Timeout time.Duration
}

type SupervisedResult struct {
	Report    *diagnostics.DiagnosticReport `json:"report,omitempty"`
	ExitCode  int                           `json:"exit_code"`
	TimedOut  bool                          `json:"timed_out,omitempty"`
	Succeeded bool                          `json:"succeeded"`
}

type OutputBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func NewOutputBuffer(maxLines int) *OutputBuffer {
	return &OutputBuffer{
		lines: make([]string, 0, maxLines),
		max:   maxLines,
	}
}

func (b *OutputBuffer) Append(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.max > 0 && len(b.lines) >= b.max {
		b.lines = b.lines[1:]
	}
	b.lines = append(b.lines, line)
}

func (b *OutputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}

func SuperviseCommand(ctx context.Context, projectRoot string, command []string, opts SuperviseOptions) (*SupervisedResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("no command specified for supervisor")
	}

	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if projectRoot != "" {
		cmd.Dir = projectRoot
	}
	cmd.Env = os.Environ()

	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return terminateProcessTree(cmd) }

	cmd.WaitDelay = 5 * time.Second

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	buf := NewOutputBuffer(500)
	var wg sync.WaitGroup
	wg.Add(2)
	go streamPipe(&wg, stdoutPipe, opts.Stdout, buf)
	go streamPipe(&wg, stderrPipe, opts.Stderr, buf)

	wg.Wait()
	cmdErr := cmd.Wait()

	captured := buf.String()
	report := diagnostics.AnalyzeErrorOutput(captured, projectRoot)
	if report != nil {
		_ = diagnostics.SaveReport(report, projectRoot)
	}

	result := &SupervisedResult{
		Report:   report,
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
	}

	var exitErr *exec.ExitError
	switch {
	case cmdErr == nil:
		result.Succeeded = true
	case errors.As(cmdErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
	default:
		result.ExitCode = -1
	}

	if result.TimedOut {
		return result, fmt.Errorf("command exceeded the %s supervision timeout", opts.Timeout)
	}
	return result, cmdErr
}

func streamPipe(wg *sync.WaitGroup, r io.Reader, out io.Writer, buf *OutputBuffer) {
	defer wg.Done()

	reader := bufio.NewReaderSize(r, 64*1024)
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		line := current.String()
		current.Reset()
		buf.Append(line)
		fmt.Fprintln(out, line)
	}

	for {
		chunk, err := reader.ReadString('\n')
		if len(chunk) > 0 {
			trimmed := strings.TrimRight(chunk, "\n")
			if current.Len()+len(trimmed) <= maxSupervisedLine {
				current.WriteString(trimmed)
			} else if current.Len() < maxSupervisedLine {
				current.WriteString(trimmed[:maxSupervisedLine-current.Len()])
			}
			if strings.HasSuffix(chunk, "\n") {
				flush()
			}
		}
		if err != nil {
			flush()
			return
		}
	}
}

func RunSupervised(ctx context.Context, projectRoot string, command []string) error {
	result, err := SuperviseCommand(ctx, projectRoot, command, SuperviseOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if result != nil && result.Report != nil {
		fmt.Fprint(os.Stderr, diagnostics.RenderDiagnosticBox(result.Report))
	}
	return err
}
