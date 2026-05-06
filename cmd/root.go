package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/app"
	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func Execute() error {
	args := os.Args[1:]
	resume := false
	if len(args) != 0 && args[0] == "resume" {
		resume = true
		args = args[1:]
	}

	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig")
	contextName := flags.String("context", "", "kubeconfig context to load")
	namespace := flags.String("namespace", "", "initial namespace filter")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("unexpected args: %v", flags.Args())
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store := state.NewStore()
	manager, err := cluster.NewManager(store, cluster.Config{
		KubeconfigPath: *kubeconfig,
		Context:        *contextName,
	})
	if err != nil {
		return err
	}
	defer closeManagerWithTimeout(manager, 5*time.Second)

	program := tea.NewProgram(
		app.New(store, manager, app.Config{InitialNamespace: *namespace, KubeconfigPath: *kubeconfig, Resume: resume}),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	)
	if _, err := program.Run(); err != nil {
		if errors.Is(err, tea.ErrProgramPanic) {
			return fmt.Errorf("fatal UI panic: %w", err)
		}
		return err
	}
	return nil
}

func closeManagerWithTimeout(manager *cluster.Manager, timeout time.Duration) {
	if manager == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Close()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		fmt.Fprintf(os.Stderr, "fatal: timed out shutting down cluster manager after %s\n", timeout)
	}
}
