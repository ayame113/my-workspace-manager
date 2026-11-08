package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ayame113/my-workspace-manager/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.OSEnv())
	stop()
	os.Exit(code)
}
