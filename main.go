package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"otto/internal/storage"
	"otto/internal/tui"
)

func main() {
	s := storage.NewDirStorage("")
	if err := s.EnsureDir(); err != nil {
		fmt.Fprintf(os.Stderr, "otto: failed to create notes directory: %v\n", err)
		os.Exit(1)
	}

	m := tui.New(s)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "otto: %v\n", err)
		os.Exit(1)
	}
}
