package main

import (
	"fmt"
	"os"

	"github.com/filecoin-project/lotus/build"
	"github.com/strahe/fil-terminator/version"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:                 "fil-terminator",
		Usage:                "Filecoin miner sector termination fee calculation tool",
		EnableBashCompletion: true,
		Version:              fmt.Sprintf("%s+lotus-%s", version.CurrentCommit, build.NodeBuildVersion),
		Commands: []*cli.Command{
			calCmd,
			batchCmd,
			toolsCmd,
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
