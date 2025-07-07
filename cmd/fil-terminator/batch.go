package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/strahe/fil-terminator/pkg/utils"
	"github.com/urfave/cli/v2"
)

var batchCmd = &cli.Command{
	Name:        "batch",
	Usage:       "Batch calculate termination fees from CSV file",
	Description: "Read miners and epochs from CSV file and calculate termination fees for all sectors",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "input",
			Aliases:  []string{"i"},
			Usage:    "Input CSV file path (format: minerid,epoch)",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "Output CSV file path (optional, print to terminal if not specified)",
		},

		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Verbose output",
		},
		&cli.StringFlag{
			Name:  "api-url",
			Usage: "Lotus API URL",
		},
	},
	Action: batchCalculate,
}

type MinerTask struct {
	MinerID string
	Epoch   abi.ChainEpoch
}

type MinerResult struct {
	MinerID        string
	Epoch          abi.ChainEpoch
	TotalSectors   int
	ActiveSectors  int
	ExpiredSectors int
	TotalFee       big.Int
	Status         string
	Error          string
}

func batchCalculate(c *cli.Context) error {
	api, closer, err := lcli.GetFullNodeAPIV1(c)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node: %w", err)
	}
	defer closer()

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	// Read CSV file
	tasks, err := readCSVFile(c.String("input"))
	if err != nil {
		return fmt.Errorf("failed to read CSV file: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no tasks found in CSV file")
	}

	fmt.Printf("Processing %d miners...\n", len(tasks))

	// Process each miner
	results := make([]MinerResult, 0, len(tasks))
	totalFee := big.Zero()

	for i, task := range tasks {
		if c.Bool("verbose") {
			fmt.Printf("[%d/%d] Processing miner %s at epoch %d...\n", i+1, len(tasks), task.MinerID, task.Epoch)
		}

		result := calculateMinerFee(ctx, api, task)
		results = append(results, result)

		if result.Error == "" {
			totalFee = big.Add(totalFee, result.TotalFee)
		}
	}

	// Output results
	if c.String("output") != "" {
		err = writeCSVResults(c.String("output"), results)
		if err != nil {
			return fmt.Errorf("failed to write output CSV: %w", err)
		}
		fmt.Printf("Results written to %s\n", c.String("output"))
	} else {
		printResults(results)
	}

	// Print summary
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total miners processed: %d\n", len(results))

	successCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		}
	}

	fmt.Printf("Successful calculations: %d\n", successCount)
	fmt.Printf("Failed calculations: %d\n", len(results)-successCount)
	fmt.Printf("Total termination fee: %s\n", types.FIL(totalFee))

	return nil
}

func readCSVFile(filename string) ([]MinerTask, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	tasks := make([]MinerTask, 0, len(records))
	for i, record := range records {
		// Skip header row if it exists
		if i == 0 && (strings.ToLower(record[0]) == "minerid" || strings.ToLower(record[0]) == "miner") {
			continue
		}

		if len(record) < 2 {
			return nil, fmt.Errorf("invalid CSV format at line %d: expected 2 columns", i+1)
		}

		minerID := strings.TrimSpace(record[0])
		epochStr := strings.TrimSpace(record[1])

		epoch, err := strconv.ParseInt(epochStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid epoch at line %d: %s", i+1, epochStr)
		}

		tasks = append(tasks, MinerTask{
			MinerID: minerID,
			Epoch:   abi.ChainEpoch(epoch),
		})
	}

	return tasks, nil
}

func calculateMinerFee(ctx context.Context, api api.FullNode, task MinerTask) MinerResult {
	// Prepare calculation request
	req := utils.CalculationRequest{
		MinerID:       task.MinerID,
		TargetEpoch:   task.Epoch,
		SectorNumbers: []abi.SectorNumber{}, // empty means all sectors
	}

	// Calculate termination fees
	calcResult := utils.CalculateTerminationFee(ctx, api, req)

	// Convert to MinerResult
	result := MinerResult{
		MinerID:        calcResult.MinerID,
		Epoch:          calcResult.TargetEpoch,
		TotalSectors:   calcResult.TotalSectors,
		ActiveSectors:  calcResult.ActiveSectors,
		ExpiredSectors: calcResult.ExpiredSectors,
		TotalFee:       calcResult.TotalFee,
		Error:          calcResult.Error,
	}

	if calcResult.Error == "" {
		result.Status = "success"
	} else {
		result.Status = "failed"
	}

	return result
}

func writeCSVResults(filename string, results []MinerResult) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"MinerID", "Epoch", "Status", "TotalSectors", "ActiveSectors", "ExpiredSectors", "TotalFee(FIL)", "Error"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write results
	for _, result := range results {
		record := []string{
			result.MinerID,
			fmt.Sprintf("%d", result.Epoch),
			result.Status,
			fmt.Sprintf("%d", result.TotalSectors),
			fmt.Sprintf("%d", result.ActiveSectors),
			fmt.Sprintf("%d", result.ExpiredSectors),
			types.FIL(result.TotalFee).String(),
			result.Error,
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}

func printResults(results []MinerResult) {
	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("%-12s %-10s %-8s %-6s %-6s %-8s %-15s %s\n",
		"MinerID", "Epoch", "Status", "Total", "Active", "Expired", "Fee(FIL)", "Error")
	fmt.Println(strings.Repeat("-", 80))

	for _, result := range results {
		errorMsg := result.Error
		if len(errorMsg) > 20 {
			errorMsg = errorMsg[:20] + "..."
		}

		fmt.Printf("%-12s %-10d %-8s %-6d %-6d %-8d %-15s %s\n",
			result.MinerID,
			result.Epoch,
			result.Status,
			result.TotalSectors,
			result.ActiveSectors,
			result.ExpiredSectors,
			types.FIL(result.TotalFee).String(),
			errorMsg,
		)
	}
}
