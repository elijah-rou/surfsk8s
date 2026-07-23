package app

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHeadlessProgramStartupQuit(t *testing.T) {
	h := newModelHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := bytes.NewBufferString("q")
	program := tea.NewProgram(
		h.app,
		tea.WithInput(input),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignals(),
		tea.WithContext(ctx),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program.Run error: %v", err)
		}
	case <-ctx.Done():
		program.Kill()
		t.Fatalf("program did not quit before deadline")
	}

	closed := make(chan struct{})
	go func() {
		h.manager.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("manager.Close did not complete after headless quit")
	}
}
