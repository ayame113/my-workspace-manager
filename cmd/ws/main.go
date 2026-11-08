package main

import (
	"context"
	"os"

	"github.com/ayame113/my-workspace-manager/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], cli.OSEnv()))
}
