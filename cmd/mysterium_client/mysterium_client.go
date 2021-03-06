package main

import (
	"fmt"
	"github.com/mysterium/node/cmd"
	"github.com/mysterium/node/cmd/commands/cli"
	"github.com/mysterium/node/cmd/commands/client"
	tequilapi_client "github.com/mysterium/node/tequilapi/client"
	"os"
	"path/filepath"
)

func main() {
	options, err := client.ParseArguments(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if options.CLI {
		runCLI(options)
	} else {
		runCMD(options)
	}
}

func runCLI(options client.CommandOptions) {
	cmdCli := cli.NewCommand(
		filepath.Join(options.DirectoryRuntime, ".cli_history"),
		tequilapi_client.NewClient(options.TequilapiAddress, options.TequilapiPort),
		newStopHandler(),
	)
	cmd.NewTerminator(cmdCli)
	if err := cmdCli.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCMD(options client.CommandOptions) {
	cmdRun := client.NewCommand(options)

	if err := cmdRun.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := cmdRun.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cmd.NewTerminator(cmdRun)
}

func newStopHandler() func() error {
	return func() error {
		os.Exit(0)
		return nil
	}
}
