package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
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
		{
			Name:    "sector-expiration",
			Aliases: []string{"exp"},
			Usage:   "Calculate sector expiration distribution",
			Action:  sectorExpirationAction,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "miner",
					Aliases: []string{"m"},
					Usage:   "Single miner address",
				},
				&cli.StringFlag{
					Name:  "miners",
					Usage: "Comma-separated list of miner addresses",
				},
				&cli.StringFlag{
					Name:    "file",
					Aliases: []string{"f"},
					Usage:   "File containing miner addresses (plain text: one per line, or CSV with 'minerid'/'miner' column)",
				},
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "Output CSV file path (optional)",
				},
				&cli.Int64Flag{
					Name:  "epoch",
					Usage: "Reference epoch (default: current)",
				},
				&cli.BoolFlag{
					Name:    "verbose",
					Aliases: []string{"v"},
					Usage:   "Verbose output",
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

type ExpirationStats struct {
	ExpirationDate string
	SectorCount    int
	Miners         map[string]int
}

type MinerExpirationData struct {
	MinerID   string
	Sectors   int
	ExpiryMap map[string]int // date -> sector count
}

func sectorExpirationAction(cctx *cli.Context) error {
	// Validate flags
	minerFlag := cctx.String("miner")
	minersFlag := cctx.String("miners")
	fileFlag := cctx.String("file")

	flagCount := 0
	if minerFlag != "" {
		flagCount++
	}
	if minersFlag != "" {
		flagCount++
	}
	if fileFlag != "" {
		flagCount++
	}

	if flagCount == 0 {
		return fmt.Errorf("must specify one of: --miner, --miners, or --file")
	}
	if flagCount > 1 {
		return fmt.Errorf("can only specify one of: --miner, --miners, or --file")
	}

	// Get miner list
	var miners []string
	var err error

	if minerFlag != "" {
		miners = []string{minerFlag}
	} else if minersFlag != "" {
		miners = strings.Split(minersFlag, ",")
		for i, m := range miners {
			miners[i] = strings.TrimSpace(m)
		}
	} else {
		miners, err = readMinersFromFile(fileFlag)
		if err != nil {
			return fmt.Errorf("failed to read miners from file: %w", err)
		}
	}

	if len(miners) == 0 {
		return fmt.Errorf("no miners found")
	}

	// Connect to Lotus API
	api, closer, err := lcli.GetFullNodeAPIV1(cctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node: %w", err)
	}
	defer closer()

	ctx, cancel := context.WithCancel(cctx.Context)
	defer cancel()

	// Get reference epoch
	refEpoch := abi.ChainEpoch(cctx.Int64("epoch"))
	if refEpoch == 0 {
		ts, err := api.ChainHead(ctx)
		if err != nil {
			return fmt.Errorf("failed to get current height: %w", err)
		}
		refEpoch = ts.Height()
	}

	fmt.Printf("Processing %d miners at reference epoch %d...\n", len(miners), refEpoch)

	// Get genesis time for date calculation
	genesisTime, err := getGenesisTime(cctx, false)
	if err != nil {
		return fmt.Errorf("failed to get genesis time: %w", err)
	}

	// Collect expiration data
	minerData := make([]MinerExpirationData, 0, len(miners))
	overallStats := make(map[string]*ExpirationStats)

	for i, minerStr := range miners {
		if cctx.Bool("verbose") {
			fmt.Printf("[%d/%d] Processing miner %s...\n", i+1, len(miners), minerStr)
		}

		data, err := getMinerExpirationData(ctx, api, minerStr, refEpoch, genesisTime)
		if err != nil {
			fmt.Printf("Warning: Failed to process miner %s: %v\n", minerStr, err)
			continue
		}

		minerData = append(minerData, data)

		// Update overall statistics
		for dateStr, count := range data.ExpiryMap {
			if overallStats[dateStr] == nil {
				overallStats[dateStr] = &ExpirationStats{
					ExpirationDate: dateStr,
					SectorCount:    0,
					Miners:         make(map[string]int),
				}
			}
			overallStats[dateStr].SectorCount += count
			overallStats[dateStr].Miners[data.MinerID] = count
		}
	}

	// Output results
	if cctx.String("output") != "" {
		err = writeExpirationCSV(cctx.String("output"), minerData, overallStats)
		if err != nil {
			return fmt.Errorf("failed to write output CSV: %w", err)
		}
		fmt.Printf("Results written to %s\n", cctx.String("output"))
	} else {
		printExpirationResults(minerData, overallStats, cctx.Bool("verbose"))
	}

	return nil
}

func readMinersFromFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Try to detect if it's a CSV file
	if isCSVFile(filename) {
		return readMinersFromCSV(file)
	}

	// Plain text format (existing behavior)
	var miners []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			miners = append(miners, line)
		}
	}

	return miners, scanner.Err()
}

func isCSVFile(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".csv")
}

func readMinersFromCSV(file *os.File) ([]string, error) {
	// Reset file position to beginning
	file.Seek(0, 0)

	reader := csv.NewReader(file)
	// Make CSV reader more lenient with field count mismatches
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	// Find minerid column
	header := records[0]
	mineridIndex := -1

	// Look for minerid/miner column (case insensitive)
	for i, col := range header {
		colLower := strings.ToLower(strings.TrimSpace(col))
		if colLower == "minerid" || colLower == "miner" {
			mineridIndex = i
			break
		}
	}

	if mineridIndex == -1 {
		return nil, fmt.Errorf("CSV file must contain a 'minerid' or 'miner' column")
	}

	miners := make([]string, 0)
	// Skip header row, start from index 1
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) <= mineridIndex {
			continue // Skip rows that don't have enough columns
		}

		minerID := strings.TrimSpace(record[mineridIndex])
		if minerID != "" {
			miners = append(miners, minerID)
		}
	}

	return miners, nil
}

func getMinerExpirationData(ctx context.Context, api api.FullNode, minerStr string, refEpoch abi.ChainEpoch, genesisTime time.Time) (MinerExpirationData, error) {
	// Parse miner address
	mid, err := address.NewFromString(minerStr)
	if err != nil {
		return MinerExpirationData{}, fmt.Errorf("invalid miner address: %w", err)
	}

	// Get tipset for reference epoch
	ts, err := api.ChainGetTipSetByHeight(ctx, refEpoch, types.EmptyTSK)
	if err != nil {
		return MinerExpirationData{}, fmt.Errorf("failed to get tipset: %w", err)
	}

	// Get all sectors
	sectors, err := api.StateMinerSectors(ctx, mid, nil, ts.Key())
	if err != nil {
		return MinerExpirationData{}, fmt.Errorf("failed to get sectors: %w", err)
	}

	// Calculate expiration distribution
	expiryMap := make(map[string]int)
	for _, sector := range sectors {
		expirationTime := utils.EpochToTime(sector.Expiration, genesisTime)
		dateStr := expirationTime.Format("2006-01-02")
		expiryMap[dateStr]++
	}

	return MinerExpirationData{
		MinerID:   minerStr,
		Sectors:   len(sectors),
		ExpiryMap: expiryMap,
	}, nil
}

func printExpirationResults(minerData []MinerExpirationData, overallStats map[string]*ExpirationStats, verbose bool) {
	fmt.Printf("\n=== Sector Expiration Distribution ===\n")

	if verbose {
		// Print per-miner details
		fmt.Printf("\n--- Per Miner Details ---\n")
		for _, data := range minerData {
			fmt.Printf("\nMiner: %s (Total sectors: %d)\n", data.MinerID, data.Sectors)

			// Sort dates
			var dates []string
			for dateStr := range data.ExpiryMap {
				dates = append(dates, dateStr)
			}
			sort.Strings(dates)

			for _, dateStr := range dates {
				count := data.ExpiryMap[dateStr]
				fmt.Printf("  Expires on %s: %d sectors\n", dateStr, count)
			}
		}
	}

	// Print overall statistics
	fmt.Printf("\n--- Overall Distribution ---\n")
	fmt.Printf("%-12s %-10s %-10s\n", "Date", "Sectors", "Miners")
	fmt.Println(strings.Repeat("-", 35))

	// Sort dates
	var dates []string
	for dateStr := range overallStats {
		dates = append(dates, dateStr)
	}
	sort.Strings(dates)

	totalSectors := 0
	for _, dateStr := range dates {
		stats := overallStats[dateStr]
		totalSectors += stats.SectorCount

		fmt.Printf("%-12s %-10d %-10d\n", dateStr, stats.SectorCount, len(stats.Miners))
	}

	fmt.Printf("\nTotal sectors: %d\n", totalSectors)
	fmt.Printf("Total miners: %d\n", len(minerData))
}

func writeExpirationCSV(filename string, minerData []MinerExpirationData, overallStats map[string]*ExpirationStats) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write overall distribution
	if err := writer.Write([]string{"=== Overall Distribution ==="}); err != nil {
		return err
	}

	if err := writer.Write([]string{"Date", "Sectors", "Miners"}); err != nil {
		return err
	}

	// Sort dates
	var dates []string
	for dateStr := range overallStats {
		dates = append(dates, dateStr)
	}
	sort.Strings(dates)

	for _, dateStr := range dates {
		stats := overallStats[dateStr]
		if err := writer.Write([]string{
			dateStr,
			strconv.Itoa(stats.SectorCount),
			strconv.Itoa(len(stats.Miners)),
		}); err != nil {
			return err
		}
	}

	// Write per-miner details
	if err := writer.Write([]string{""}); err != nil {
		return err
	}
	if err := writer.Write([]string{"=== Per Miner Details ==="}); err != nil {
		return err
	}

	for _, data := range minerData {
		if err := writer.Write([]string{fmt.Sprintf("Miner: %s", data.MinerID)}); err != nil {
			return err
		}
		if err := writer.Write([]string{"Date", "Sectors"}); err != nil {
			return err
		}

		// Sort dates for this miner
		var minerDates []string
		for dateStr := range data.ExpiryMap {
			minerDates = append(minerDates, dateStr)
		}
		sort.Strings(minerDates)

		for _, dateStr := range minerDates {
			count := data.ExpiryMap[dateStr]
			if err := writer.Write([]string{
				dateStr,
				strconv.Itoa(count),
			}); err != nil {
				return err
			}
		}

		if err := writer.Write([]string{""}); err != nil {
			return err
		}
	}

	return nil
}
