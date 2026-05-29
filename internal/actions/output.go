package actions

import (
	"fmt"
	"os"
)

// OutputWriter abstracts output so actions work in both CLI and TUI.
type OutputWriter interface {
	Info(msg string)
	Success(msg string)
	Warning(msg string)
	Error(msg string)
	Print(msg string)
}

// ClearScreen sends the ANSI escape to clear the terminal.
func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

// StdOutput writes to stdout/stderr with ANSI colors.
type StdOutput struct{}

func (s *StdOutput) Info(msg string)    { fmt.Fprintf(os.Stdout, "\033[0;34m[*]\033[0m %s\n", msg) }
func (s *StdOutput) Success(msg string) { fmt.Fprintf(os.Stdout, "\033[0;32m[+]\033[0m %s\n", msg) }
func (s *StdOutput) Warning(msg string) { fmt.Fprintf(os.Stderr, "\033[1;33m[!]\033[0m %s\n", msg) }
func (s *StdOutput) Error(msg string)   { fmt.Fprintf(os.Stderr, "\033[0;31m[-]\033[0m %s\n", msg) }
func (s *StdOutput) Print(msg string)   { fmt.Fprintln(os.Stdout, msg) }
