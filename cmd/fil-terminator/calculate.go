package main

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/strahe/fil-terminator/pkg/utils"
	"github.com/urfave/cli/v2"
)

var calCmd = &cli.Command{
	Name:        "calculate",
	Aliases:     []string{"calc"},
	Usage:       "Calculate termination fees",
	Description: "Calculate termination fees for miner sectors, supporting historical data queries and future estimation.",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "miner",
			Aliases:  []string{"m"},
			Usage:    "Miner address",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "sectors",
			Aliases: []string{"s"},
			Usage:   "Sector number list, comma separated (e.g. 1,2,3 or 1-10)",
		},
		&cli.BoolFlag{
			Name:    "all",
			Aliases: []string{"a"},
			Usage:   "Calculate all sectors",
		},
		&cli.Int64Flag{
			Name:    "epoch",
			Aliases: []string{"e"},
			Usage:   "Target epoch, use current height if not specified",
		},

		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Verbose output",
		},
	},
	Action: calculate,
}

func calculate(c *cli.Context) error {
	api, closer, err := lcli.GetFullNodeAPIV1(c)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node: %w", err)
	}
	defer closer()

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	// Check parameters
	if !c.Bool("all") && c.String("sectors") == "" {
		return fmt.Errorf("must specify --sectors or --all")
	}
	if c.Bool("all") && c.String("sectors") != "" {
		return fmt.Errorf("cannot specify both --sectors and --all")
	}

	// Prepare calculation request
	req := utils.CalculationRequest{
		MinerID:     c.String("miner"),
		TargetEpoch: abi.ChainEpoch(c.Int64("epoch")),
	}

	// Parse sector numbers if specified
	if !c.Bool("all") {
		sectorNumbers, err := utils.ParseSectorNumbers(c.String("sectors"))
		if err != nil {
			return fmt.Errorf("invalid sector numbers: %w", err)
		}
		req.SectorNumbers = sectorNumbers
	}

	// Calculate termination fees
	result := utils.CalculateTerminationFee(ctx, api, req)
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}

	// Display mode information
	if result.IsEstimate {
		epochDiff := result.TargetEpoch - result.CurrentEpoch
		daysDiff := utils.EpochsToDays(epochDiff)
		fmt.Printf("Estimation mode: predicting fees for epoch %d (+%.1f days) based on data from epoch %d\n",
			result.TargetEpoch, daysDiff, result.CurrentEpoch)
	} else {
		fmt.Printf("Calculation epoch: %d\n", result.TargetEpoch)
	}

	// Display sector details if verbose
	if c.Bool("verbose") {
		fmt.Printf("Sector details:\n")
		for _, sectorResult := range result.SectorResults {
			if sectorResult.IsExpired {
				fmt.Printf("  Sector %d: EXPIRED (expired %.1f days ago)\n",
					sectorResult.SectorNumber, sectorResult.ExpiredDays)
			} else {
				status := "historical"
				if result.IsEstimate {
					status = "estimated"
				}
				ageInDays := utils.EpochsToDays(sectorResult.Age)
				fmt.Printf("  Sector %d: %s FIL (age: %.1f days, %s)\n",
					sectorResult.SectorNumber, types.FIL(sectorResult.Fee), ageInDays, status)
			}
		}
	}

	// Display summary
	fmt.Printf("Total sectors: %d\n", result.TotalSectors)
	if result.ExpiredSectors > 0 {
		fmt.Printf("Expired sectors: %d\n", result.ExpiredSectors)
		fmt.Printf("Active sectors: %d\n", result.ActiveSectors)
	}
	fmt.Printf("Total termination fee: %s\n", types.FIL(result.TotalFee))

	return nil
}
