package main

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/ronda2009/forgeboard/internal/cli"
)

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	kernel32.NewProc("SetConsoleOutputCP").Call(65001)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || !cli.IsCommand(args[0]) {
		cli.Usage()
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "\nunknown command %q\n", args[0])
			os.Exit(2)
		}
		return
	}
	if err := cli.Run(context.Background(), args); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
