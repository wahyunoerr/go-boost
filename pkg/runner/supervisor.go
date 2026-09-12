package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/wahyunoerr/go-boost/pkg/diagnostics"
)

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

	if len(b.lines) >= b.max {
		b.lines = b.lines[1:]
	}
	b.lines = append(b.lines, line)
}

func (b *OutputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}

func SuperviseCommand(ctx context.Context, projectRoot string, command []string) (*diagnostics.DiagnosticReport, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("no command specified for supervisor")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if projectRoot != "" {
		cmd.Dir = projectRoot
	}
	cmd.Env = os.Environ()

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

	streamPipe := func(r io.Reader, out io.Writer) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			buf.Append(line)
			fmt.Fprintln(out, line)
		}
	}

	wg.Add(2)
	go streamPipe(stdoutPipe, os.Stdout)
	go streamPipe(stderrPipe, os.Stderr)

	wg.Wait()
	cmdErr := cmd.Wait()

	captured := buf.String()
	report := diagnostics.AnalyzeErrorOutput(captured, projectRoot)

	if report != nil {
		_ = diagnostics.SaveReport(report, projectRoot)
	}

	return report, cmdErr
}

func RunSupervised(ctx context.Context, projectRoot string, command []string) error {
	report, err := SuperviseCommand(ctx, projectRoot, command)
	if report != nil {
		fmt.Fprint(os.Stderr, diagnostics.RenderDiagnosticBox(report))
	}
	return err
}
