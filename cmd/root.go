package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/app"
	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func Execute() error {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig")
	contextName := flags.String("context", "", "kubeconfig context to load")
	namespace := flags.String("namespace", "", "initial namespace filter")

	if err := flags.Parse(os.Args[1:]); err != nil {
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
	defer manager.Close()

	program := tea.NewProgram(
		app.New(store, manager, app.Config{InitialNamespace: *namespace, KubeconfigPath: *kubeconfig}),
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)
	if _, err := program.Run(); err != nil {
		return err
	}
	return nil
}
