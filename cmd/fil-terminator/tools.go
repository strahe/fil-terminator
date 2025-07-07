package main

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/strahe/fil-terminator/pkg/utils"
	"github.com/urfave/cli/v2"
)

var toolsCmd = &cli.Command{
	Name:        "tools",
	Usage:       "Utility tools for epoch and time conversion",
	Description: "Various utility tools for Filecoin operations",
	Subcommands: []*cli.Command{
		{
			Name:    "epoch-to-time",
			Aliases: []string{"e2t"},
			Usage:   "Convert epoch to time",
			Action:  epochToTimeAction,
			Flags: []cli.Flag{
				&cli.Int64Flag{
					Name:     "epoch",
					Aliases:  []string{"e"},
					Usage:    "Epoch number to convert",
					Required: true,
				},
				&cli.StringFlag{
					Name:    "timezone",
					Aliases: []string{"tz"},
					Usage:   "Output timezone (default: local)",
					Value:   "local",
				},
				&cli.BoolFlag{
					Name:  "offline",
					Usage: "Use offline mode with default mainnet genesis time",
				},
			},
		},
		{
			Name:    "time-to-epoch",
			Aliases: []string{"t2e"},
			Usage:   "Convert time to epoch",
			Action:  timeToEpochAction,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "time",
					Aliases:  []string{"t"},
					Usage:    "Time string to convert (e.g., '2024-01-01 12:00:00')",
					Required: true,
				},
				&cli.BoolFlag{
					Name:  "offline",
					Usage: "Use offline mode with default mainnet genesis time",
				},
			},
		},
	},
}

func epochToTimeAction(cctx *cli.Context) error {
	epoch := cctx.Int64("epoch")
	timezone := cctx.String("timezone")
	offline := cctx.Bool("offline")

	// Get genesis time
	genesisTime, err := getGenesisTime(cctx, offline)
	if err != nil {
		return err
	}

	t := utils.EpochToTime(abi.ChainEpoch(epoch), genesisTime)

	// Handle timezone
	var displayTime time.Time
	switch timezone {
	case "local":
		displayTime = t.Local()
	case "utc":
		displayTime = t.UTC()
	default:
		// Try to parse as timezone location
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return fmt.Errorf("invalid timezone: %s", timezone)
		}
		displayTime = t.In(loc)
	}

	fmt.Printf("Epoch: %d\n", epoch)
	fmt.Printf("Time (UTC): %s\n", t.UTC().Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Time (%s): %s\n", timezone, displayTime.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Unix timestamp: %d\n", t.Unix())

	return nil
}

func timeToEpochAction(cctx *cli.Context) error {
	timeStr := cctx.String("time")
	offline := cctx.Bool("offline")

	// Get genesis time
	genesisTime, err := getGenesisTime(cctx, offline)
	if err != nil {
		return err
	}

	t, err := utils.ParseTime(timeStr)
	if err != nil {
		return fmt.Errorf("failed to parse time: %w", err)
	}

	epoch := utils.TimeToEpoch(t, genesisTime)

	fmt.Printf("Time: %s\n", t.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Time (UTC): %s\n", t.UTC().Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Epoch: %d\n", epoch)
	fmt.Printf("Unix timestamp: %d\n", t.Unix())

	return nil
}

// getGenesisTime retrieves genesis time either from API or uses default mainnet time
func getGenesisTime(cctx *cli.Context, offline bool) (time.Time, error) {
	// Default mainnet genesis time: 2020-08-24 22:00:00 UTC
	defaultGenesisTime := time.Date(2020, 8, 24, 22, 0, 0, 0, time.UTC)

	if offline {
		fmt.Println("Using offline mode with default mainnet genesis time")
		return defaultGenesisTime, nil
	}

	// Try to connect to Lotus API
	api, closer, err := lcli.GetFullNodeAPIV1(cctx)
	if err != nil {
		fmt.Printf("Warning: Failed to connect to Lotus node (%v), using default mainnet genesis time\n", err)
		return defaultGenesisTime, nil
	}
	defer closer()

	ctx, cancel := context.WithCancel(cctx.Context)
	defer cancel()

	// Get genesis time from API
	genesis, err := api.ChainGetGenesis(ctx)
	if err != nil {
		fmt.Printf("Warning: Failed to get genesis from API (%v), using default mainnet genesis time\n", err)
		return defaultGenesisTime, nil
	}

	genesisTime := time.Unix(int64(genesis.Blocks()[0].Timestamp), 0)
	return genesisTime, nil
}
