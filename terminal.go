package main

import (
	"errors"
	"io"
	"sync"
)

const (
	beginSynchronizedOutput = "\x1b[?2026h"
	endSynchronizedOutput   = "\x1b[?2026l"
	enableGraphemeClusters  = "\x1b[?2027h"
	disableGraphemeClusters = "\x1b[?2027l"
)

var terminalWriteMu sync.Mutex

// synchronizedWriter makes a complete Bubble Tea repaint visible at once on
// terminals that implement DEC synchronized output. Unsupported terminals
// safely ignore the control sequences.
type synchronizedWriter struct {
	w io.Writer
}

func newSynchronizedWriter(w io.Writer) *synchronizedWriter {
	return &synchronizedWriter{w: w}
}

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	terminalWriteMu.Lock()
	defer terminalWriteMu.Unlock()

	if _, err := io.WriteString(w.w, beginSynchronizedOutput); err != nil {
		return 0, err
	}
	n, writeErr := w.w.Write(p)
	_, endErr := io.WriteString(w.w, endSynchronizedOutput)
	if writeErr != nil {
		return n, writeErr
	}
	if endErr != nil {
		return n, endErr
	}
	return n, nil
}

// Bubble Tea queries its output for a file descriptor to detect terminal
// dimensions. Forward the terminal-file methods when the wrapped writer has
// them so WithOutput does not disable resize events.
func (w *synchronizedWriter) Read(p []byte) (int, error) {
	if r, ok := w.w.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, errors.New("terminal output is not readable")
}

func (w *synchronizedWriter) Close() error {
	return nil
}

func (w *synchronizedWriter) Fd() uintptr {
	if f, ok := w.w.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

func writeTerminalSequence(w io.Writer, sequence string) error {
	terminalWriteMu.Lock()
	defer terminalWriteMu.Unlock()

	if _, err := io.WriteString(w, beginSynchronizedOutput); err != nil {
		return err
	}
	if _, err := io.WriteString(w, sequence); err != nil {
		return err
	}
	_, err := io.WriteString(w, endSynchronizedOutput)
	return err
}
