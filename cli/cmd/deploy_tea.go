package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// terminalFile returns w as an *os.File if it is one, so the terminal probes
// (isatty, size) have a file descriptor to work with. A wrapped or buffer writer
// is not a terminal.
func terminalFile(w io.Writer) (*os.File, bool) {
	f, ok := w.(*os.File)
	return f, ok
}

// interactiveWriter reports whether the animated deploy view can run: w must be
// a terminal to render, and stdin must be a terminal too because the view reads
// raw-mode keys (Ctrl-C). If either end is redirected (pipes, CI, tests) the
// plain streaming sink renders instead - the same terminal gate coloured output
// uses, plus the stdin check the interactive input needs.
func interactiveWriter(w io.Writer) bool {
	f, ok := terminalFile(w)
	return ok && isatty.IsTerminal(f.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
}

// Messages driving the deploy view: a projection's RPC began or finished. The
// work loop sends these into the program from the main goroutine while the
// program renders in its own; the model quits itself once the last row commits.
type (
	deployStartMsg struct{ name string }
	deployDoneMsg  struct{ res deployResult }
)

type rowStatus int

const (
	rowPending rowStatus = iota
	rowActive
	rowDone
)

// deployRow is one projection's line in the interactive view: its name, where it
// is in the run, and its outcome once done.
type deployRow struct {
	name   string
	status rowStatus
	res    deployResult
}

// teaModel is the interactive deploy view. Each projection completes top to
// bottom; a finished row commits to scrollback (so the full record survives any
// terminal height) and the live region holds a bounded window of the active row
// plus the next pending ones, capped to the terminal height, with a summary
// tally underneath.
// rows and index share their backing array/map across the value copies bubbletea
// makes of the model, so Update only ever mutates rows[i] in place - it must not
// append to rows (that would reallocate and desync the copies).
type teaModel struct {
	spinner   spinner.Model
	tw        *textWriter
	nameWidth int
	rows      []deployRow
	index     map[string]int
	counts    deployCounts
	committed int                // rows [0:committed) printed to scrollback, no longer live
	height    int                // terminal rows; 0 until the first size is known
	cancel    context.CancelFunc // stops the deploy when the user interrupts the view
}

func (m teaModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		// The terminal is in raw mode, so Ctrl-C is a keystroke, not a signal
		// (main's signal handler never sees it): cancel the deploy, stopping the
		// in-flight RPC and the loop, then quit.
		if msg.Type == tea.KeyCtrlC {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		return m, nil
	case deployStartMsg:
		m.rows[m.index[msg.name]].status = rowActive
		return m, nil
	case deployDoneMsg:
		i := m.index[msg.res.Name]
		m.rows[i].status = rowDone
		m.rows[i].res = msg.res
		m.counts.add(msg.res)
		// Commit every now-finished row at the front of the window to scrollback.
		// Deploys complete in order, so this is the row that just finished.
		var lines []string
		for m.committed < len(m.rows) && m.rows[m.committed].status == rowDone {
			lines = append(lines, m.tw.deployResultLine(m.rows[m.committed].res, m.nameWidth))
			m.committed++
		}
		var cmd tea.Cmd
		if len(lines) > 0 {
			cmd = tea.Println(strings.Join(lines, "\n"))
		}
		if m.committed == len(m.rows) {
			// Last rows committed: print this final batch and quit in one
			// sequence, so the quit can't outrace the print and drop the trailing
			// lines. A separate quit message did exactly that on a one-projection
			// deploy - its print and quit ran as independent racing commands.
			return m, tea.Sequence(cmd, tea.Quit)
		}
		return m, cmd
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

// liveCap is how many rows the live window may show, leaving room beneath for the
// blank line, the summary, and the sacrificial trailing line. Zero height (before
// the first size message) means show everything.
func (m teaModel) liveCap() int {
	if m.height <= 0 {
		return len(m.rows) - m.committed
	}
	if avail := m.height - 3; avail >= 2 {
		return avail
	}
	return 2
}

func (m teaModel) View() string {
	var b strings.Builder

	remaining := m.rows[m.committed:]
	show := len(remaining)
	hidden := 0
	if limit := m.liveCap(); show > limit {
		show = limit - 1 // last line reserved for the "more" indicator
		hidden = len(remaining) - show
	}
	for _, row := range remaining[:show] {
		b.WriteString(m.tw.deployRowLine(row, m.spinner.View(), m.nameWidth))
		b.WriteByte('\n')
	}
	if hidden > 0 {
		b.WriteString(m.tw.styles.pipe.Render(fmt.Sprintf("  … %d more", hidden)))
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString(m.tw.deploySummaryLine(m.counts))
	// Trailing newline so the summary isn't the literal last line: bubbletea's
	// renderer erases the final line on quit, so the sacrificial line must be
	// this empty one, not the summary.
	b.WriteByte('\n')
	return b.String()
}

// teaSink runs the interactive view in its own goroutine and feeds it events
// from the work loop. finish signals completion and blocks until the program's
// final frame is rendered and it exits.
type teaSink struct {
	prog   *tea.Program
	exited chan error
}

func newTeaSink(w io.Writer, names []string, ctx context.Context, cancel context.CancelFunc) *teaSink {
	tw := newTextWriter(w, w)
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = tw.styles.label

	rows := make([]deployRow, len(names))
	index := make(map[string]int, len(names))
	for i, n := range names {
		rows[i] = deployRow{name: n, status: rowPending}
		index[n] = i
	}

	m := teaModel{
		spinner:   sp,
		tw:        tw,
		nameWidth: maxNameWidth(names),
		rows:      rows,
		index:     index,
		height:    initialHeight(w),
		cancel:    cancel,
	}
	// Bind the program to the deploy context: if it is cancelled by anything
	// other than the interactive Ctrl-C (a signal, a future deadline), the
	// program is killed and finish() unblocks, rather than waiting forever for an
	// auto-quit that a half-finished run will never reach.
	p := tea.NewProgram(m, tea.WithOutput(w), tea.WithContext(ctx))
	s := &teaSink{prog: p, exited: make(chan error, 1)}
	go func() {
		_, err := p.Run()
		s.exited <- err
	}()
	return s
}

// initialHeight reads the terminal height up front so the first frame is already
// paged correctly, before bubbletea's WindowSizeMsg arrives. 0 if unavailable.
func initialHeight(w io.Writer) int {
	f, ok := terminalFile(w)
	if !ok {
		return 0
	}
	if _, h, err := term.GetSize(int(f.Fd())); err == nil {
		return h
	}
	return 0
}

func (s *teaSink) start(name string, _, _ int) { s.prog.Send(deployStartMsg{name: name}) }

func (s *teaSink) done(res deployResult) { s.prog.Send(deployDoneMsg{res: res}) }

// finish waits for the program to exit. The model quits itself on completion
// (the last row's commit sequences a quit), and the program is otherwise killed
// by Ctrl-C or by the deploy context being cancelled (a signal). Neither
// interrupt is a sink failure - the context is already cancelled, so runDeploy
// reports it from ctx.Err() and exits cleanly; only a genuine render error
// surfaces here.
func (s *teaSink) finish() error {
	err := <-s.exited
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}
