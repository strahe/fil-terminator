package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	stactors "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	stactorsminer "github.com/filecoin-project/go-state-types/builtin/v16/miner"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/strahe/fil-terminator/pkg/utils"
	"github.com/urfave/cli/v2"
)

var batch1Cmd = &cli.Command{
	Name:  "batch1",
	Usage: "Smart batch calculation with termination strategy optimization",
	Description: `Calculate optimal termination strategy by comparing immediate termination vs natural expiration costs.

EXAMPLES:
   # Use CSV file with threshold optimization (7 days)
   fil-terminator batch1 -i miners.csv -e 2500000 -t 7

   # Single miner with 10-day threshold
   fil-terminator batch1 -i f01234 -e 2500000 -t 10

   # Disable optimization, terminate all sectors at specified epoch
   fil-terminator batch1 -i miners.csv -e 2500000 -t 0

   # Verbose output with results export
   fil-terminator batch1 -i miners.csv -e 2500000 -t 7 -v -o results.csv

STRATEGY:
   - When threshold > 0: Sectors expiring before (termination-epoch + threshold days) will expire naturally, others will be terminated
   - When threshold = 0: All sectors will be terminated at the specified epoch (no optimization)
   - Example: threshold=7 means sectors expiring within 7 days after termination epoch will expire naturally
   - The tool calculates both termination and expiration costs to find the optimal strategy`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "input",
			Aliases:  []string{"i"},
			Usage:    "Input CSV file path with minerid column, or single miner ID",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "Output CSV file path (optional, print to terminal if not specified)",
		},
		&cli.Int64Flag{
			Name:     "termination-epoch",
			Aliases:  []string{"epoch", "e"},
			Usage:    "Target termination epoch for all miners",
			Required: true,
		},
		&cli.IntFlag{
			Name:    "expiration-threshold",
			Aliases: []string{"threshold", "t"},
			Usage:   "Days threshold: sectors expiring before (termination-epoch + threshold days) will expire naturally, others will be terminated (0 = disable optimization, terminate all)",
			Value:   7,
		},
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Verbose output",
		},
		&cli.IntFlag{
			Name:    "workers",
			Aliases: []string{"w"},
			Usage:   "Number of concurrent workers",
			Value:   runtime.NumCPU(),
		},
		&cli.StringFlag{
			Name:  "api-url",
			Usage: "Lotus API URL",
		},
	},
	Action: batch1Calculate,
}

type MinerStrategyTask struct {
	Index               int
	MinerID             string
	TerminationEpoch    abi.ChainEpoch
	ExpirationThreshold int
	Verbose             bool
}

type StrategyResult struct {
	Index               int
	MinerID             string
	TerminationEpoch    abi.ChainEpoch
	ExpirationThreshold int
	TotalSectors        int
	TerminateSectors    int
	ExpireSectors       int
	TerminationFee      big.Int
	ExpirationFee       big.Int
	TotalFee            big.Int
	SectorDetails       []SectorStrategy
	Status              string
	Error               string
}

type SectorStrategy struct {
	SectorNumber    abi.SectorNumber
	ExpirationEpoch abi.ChainEpoch
	RemainingDays   float64
	Strategy        string // "terminate" or "expire"
	TerminationFee  big.Int
	DailyFaultFee   big.Int
	ExpirationFee   big.Int
	RecommendedFee  big.Int
}

func batch1Calculate(c *cli.Context) error {
	api, closer, err := lcli.GetFullNodeAPIV1(c)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node: %w", err)
	}
	defer closer()

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	// Parse miners
	miners, err := parseMiners(c.String("input"))
	if err != nil {
		return fmt.Errorf("failed to parse miners: %w", err)
	}

	if len(miners) == 0 {
		return fmt.Errorf("no miners found")
	}

	terminationEpoch := abi.ChainEpoch(c.Int64("termination-epoch"))
	expirationThreshold := c.Int("expiration-threshold")
	workers := c.Int("workers")
	verbose := c.Bool("verbose")

	fmt.Printf("=== Batch1 Strategy Calculation ===\n")
	fmt.Printf("Miners to process: %d\n", len(miners))
	fmt.Printf("Termination epoch: %d\n", terminationEpoch)
	if expirationThreshold == 0 {
		fmt.Printf("Expiration threshold: 0 days (optimization disabled - terminate all)\n")
	} else {
		fmt.Printf("Expiration threshold: %d days\n", expirationThreshold)
	}
	fmt.Printf("Concurrent workers: %d\n", workers)
	fmt.Printf("=====================================\n\n")

	// Process miners concurrently
	results := processMinersConcurrently(ctx, api, miners, terminationEpoch, expirationThreshold, workers, verbose)

	// Calculate totals
	totalTerminationFee := big.Zero()
	totalExpirationFee := big.Zero()
	for _, result := range results {
		if result.Error == "" {
			totalTerminationFee = big.Add(totalTerminationFee, result.TerminationFee)
			totalExpirationFee = big.Add(totalExpirationFee, result.ExpirationFee)
		}
	}

	// Output results
	if c.String("output") != "" {
		err = writeStrategyResults(c.String("output"), results)
		if err != nil {
			return fmt.Errorf("failed to write output CSV: %w", err)
		}
		fmt.Printf("Results written to %s\n", c.String("output"))
	} else {
		printStrategyResults(results, c.Bool("verbose"))
	}

	// Print summary
	printSummary(results, totalTerminationFee, totalExpirationFee)

	return nil
}

func parseMiners(input string) ([]string, error) {
	// Check if input is a file or direct miner ID
	if _, err := os.Stat(input); err == nil {
		// It's a file, read CSV
		return readMinersFromCSVFile(input)
	} else {
		// Treat as direct miner ID
		if strings.HasPrefix(input, "f0") {
			return []string{input}, nil
		}
		return nil, fmt.Errorf("invalid miner ID format: %s", input)
	}
}

func readMinersFromCSVFile(filename string) ([]string, error) {
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

	miners := make([]string, 0)
	mineridIndex := -1

	for i, record := range records {
		if i == 0 {
			// Find minerid or miner column
			for j, col := range record {
				col = strings.ToLower(strings.TrimSpace(col))
				if col == "minerid" || col == "miner" {
					mineridIndex = j
					break
				}
			}
			if mineridIndex == -1 {
				// No header, assume first column is miner ID
				mineridIndex = 0
			} else {
				continue // Skip header row
			}
		}

		if len(record) <= mineridIndex {
			continue
		}

		minerID := strings.TrimSpace(record[mineridIndex])
		if minerID != "" && strings.HasPrefix(minerID, "f0") {
			miners = append(miners, minerID)
		}
	}

	return miners, nil
}

func processMinersConcurrently(ctx context.Context, api api.FullNode, miners []string, terminationEpoch abi.ChainEpoch, expirationThreshold int, workers int, verbose bool) []StrategyResult {
	// Create tasks
	tasks := make([]MinerStrategyTask, len(miners))
	for i, minerID := range miners {
		tasks[i] = MinerStrategyTask{
			Index:               i,
			MinerID:             minerID,
			TerminationEpoch:    terminationEpoch,
			ExpirationThreshold: expirationThreshold,
			Verbose:             verbose,
		}
	}

	// Create channels
	taskCh := make(chan MinerStrategyTask, len(tasks))
	resultCh := make(chan StrategyResult, len(tasks))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for task := range taskCh {
				if verbose {
					fmt.Printf("[Worker %d] Processing miner %s [%d/%d]...\n",
						workerID, task.MinerID, task.Index+1, len(tasks))
				}
				result := calculateMinerStrategy(ctx, api, task)
				resultCh <- result
			}
		}(i)
	}

	// Send tasks
	go func() {
		for _, task := range tasks {
			taskCh <- task
		}
		close(taskCh)
	}()

	// Wait for workers to finish
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	results := make([]StrategyResult, len(miners))
	for result := range resultCh {
		results[result.Index] = result
	}

	return results
}

func calculateMinerStrategy(ctx context.Context, api api.FullNode, task MinerStrategyTask) StrategyResult {
	result := StrategyResult{
		Index:               task.Index,
		MinerID:             task.MinerID,
		TerminationEpoch:    task.TerminationEpoch,
		ExpirationThreshold: task.ExpirationThreshold,
	}

	// Parse miner address
	mid, err := address.NewFromString(task.MinerID)
	if err != nil {
		result.Error = fmt.Sprintf("invalid miner address: %v", err)
		result.Status = "failed"
		return result
	}

	// Get current tipset
	currentTs, err := api.ChainHead(ctx)
	if err != nil {
		result.Error = fmt.Sprintf("failed to get current height: %v", err)
		result.Status = "failed"
		return result
	}

	// Validate termination epoch
	if task.TerminationEpoch <= currentTs.Height() && task.TerminationEpoch != 0 {
		if task.Verbose {
			fmt.Printf("  Warning: Termination epoch %d is in the past (current: %d)\n",
				task.TerminationEpoch, currentTs.Height())
		}
	}

	// Get tipset at termination epoch
	var ts *types.TipSet
	if task.TerminationEpoch > currentTs.Height() {
		ts = currentTs // Use current for future estimation
	} else {
		ts, err = api.ChainGetTipSetByHeight(ctx, task.TerminationEpoch, types.EmptyTSK)
		if err != nil {
			result.Error = fmt.Sprintf("failed to get tipset at epoch %d: %v", task.TerminationEpoch, err)
			result.Status = "failed"
			return result
		}
	}

	// Get network parameters
	bstore := blockstore.NewAPIBlockstore(api)
	adtStore := adt.WrapStore(ctx, cbor.NewCborStore(bstore))

	nv, err := api.StateNetworkVersion(ctx, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get network version: %v", err)
		result.Status = "failed"
		return result
	}

	minerAct, err := api.StateGetActor(ctx, mid, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get miner actor: %v", err)
		result.Status = "failed"
		return result
	}

	minerInfo, err := api.StateMinerInfo(ctx, mid, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get miner info: %v", err)
		result.Status = "failed"
		return result
	}

	var minerVersion stactors.Version
	if _, version, ok := actors.GetActorMetaByCode(minerAct.Code); !ok {
		result.Error = fmt.Sprintf("unsupported miner actor code: %s", minerAct.Code)
		result.Status = "failed"
		return result
	} else {
		minerVersion = version
	}

	if minerVersion < stactors.Version16 {
		result.Error = fmt.Sprintf("unsupported miner version %d", minerVersion)
		result.Status = "failed"
		return result
	}

	// Get reward and power smoothed estimates
	rewardSmoothed, powerSmoothed, err := getNetworkParameters(ctx, api, adtStore, ts)
	if err != nil {
		result.Error = fmt.Sprintf("failed to get network parameters: %v", err)
		result.Status = "failed"
		return result
	}

	// Get all sectors
	sectors, err := api.StateMinerSectors(ctx, mid, nil, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get sectors: %v", err)
		result.Status = "failed"
		return result
	}

	result.TotalSectors = len(sectors)

	if len(sectors) == 0 {
		if task.Verbose {
			fmt.Printf("  Warning: Miner %s has no sectors\n", task.MinerID)
		}
		result.Status = "success"
		return result
	}

	// Calculate strategy for each sector
	terminationFee := big.Zero()
	expirationFee := big.Zero()
	terminateCount := 0
	expireCount := 0
	sectorDetails := make([]SectorStrategy, 0, len(sectors))

	thresholdEpochs := abi.ChainEpoch(task.ExpirationThreshold * 2880) // Convert days to epochs

	for _, sector := range sectors {
		// Skip already expired sectors
		if task.TerminationEpoch >= sector.Expiration {
			continue
		}

		sectorDetail := SectorStrategy{
			SectorNumber:    sector.SectorNumber,
			ExpirationEpoch: sector.Expiration,
			RemainingDays:   utils.EpochsToDays(sector.Expiration - task.TerminationEpoch),
		}

		// Calculate termination fee
		sectorAge := task.TerminationEpoch - sector.Activation
		faultFee, err := miner.PledgePenaltyForContinuedFault(
			nv,
			builtin.FilterEstimate{
				PositionEstimate: rewardSmoothed.PositionEstimate,
				VelocityEstimate: rewardSmoothed.VelocityEstimate,
			},
			builtin.FilterEstimate{
				PositionEstimate: powerSmoothed.PositionEstimate,
				VelocityEstimate: powerSmoothed.VelocityEstimate,
			},
			stactorsminer.QAPowerForSector(minerInfo.SectorSize, sector),
		)
		if err != nil {
			continue
		}

		termFee, err := miner.PledgePenaltyForTermination(nv, sector.InitialPledge, sectorAge, faultFee)
		if err != nil {
			continue
		}

		sectorDetail.TerminationFee = termFee
		sectorDetail.DailyFaultFee = faultFee

		// Calculate expiration fee (daily fault fee * remaining days)
		remainingEpochs := sector.Expiration - task.TerminationEpoch
		expFee := big.Mul(faultFee, big.NewInt(int64(remainingEpochs/2880))) // Daily fee * days
		sectorDetail.ExpirationFee = expFee

		// Decide strategy based on threshold
		// Logic: sectors expiring BEFORE (termination-epoch + threshold) should expire naturally
		//        sectors expiring AFTER (termination-epoch + threshold) should be terminated
		if task.ExpirationThreshold == 0 {
			// Threshold is 0: disable optimization, terminate all sectors
			sectorDetail.Strategy = "terminate"
			sectorDetail.RecommendedFee = termFee
			terminationFee = big.Add(terminationFee, termFee)
			terminateCount++
		} else if remainingEpochs <= thresholdEpochs {
			// Sector expires within threshold days after termination epoch - let it expire naturally
			sectorDetail.Strategy = "expire"
			sectorDetail.RecommendedFee = expFee
			expirationFee = big.Add(expirationFee, expFee)
			expireCount++
		} else {
			// Sector expires beyond threshold - terminate it now
			sectorDetail.Strategy = "terminate"
			sectorDetail.RecommendedFee = termFee
			terminationFee = big.Add(terminationFee, termFee)
			terminateCount++
		}

		sectorDetails = append(sectorDetails, sectorDetail)

		if task.Verbose {
			fmt.Printf("  Sector %d: %s (%.1f days) -> %s FIL\n",
				sector.SectorNumber, sectorDetail.Strategy, sectorDetail.RemainingDays,
				types.FIL(sectorDetail.RecommendedFee).String())
		}
	}

	result.TerminateSectors = terminateCount
	result.ExpireSectors = expireCount
	result.TerminationFee = terminationFee
	result.ExpirationFee = expirationFee
	result.TotalFee = big.Add(terminationFee, expirationFee)
	result.SectorDetails = sectorDetails
	result.Status = "success"

	if task.Verbose {
		fmt.Printf("  Summary: %d terminate, %d expire, total fee: %s FIL\n",
			terminateCount, expireCount, types.FIL(result.TotalFee).String())
	}

	return result
}

func getNetworkParameters(ctx context.Context, api api.FullNode, adtStore adt.Store, ts *types.TipSet) (builtin.FilterEstimate, builtin.FilterEstimate, error) {
	var rewardSmoothed, powerSmoothed builtin.FilterEstimate

	if act, err := api.StateGetActor(ctx, reward.Address, ts.Key()); err != nil {
		return rewardSmoothed, powerSmoothed, fmt.Errorf("failed to load reward actor: %v", err)
	} else if s, err := reward.Load(adtStore, act); err != nil {
		return rewardSmoothed, powerSmoothed, fmt.Errorf("failed to load reward actor state: %v", err)
	} else if rewardSmoothed, err = s.ThisEpochRewardSmoothed(); err != nil {
		return rewardSmoothed, powerSmoothed, fmt.Errorf("failed to get smoothed reward: %v", err)
	}

	if act, err := api.StateGetActor(ctx, power.Address, ts.Key()); err != nil {
		return rewardSmoothed, powerSmoothed, fmt.Errorf("failed to load power actor: %v", err)
	} else if s, err := power.Load(adtStore, act); err != nil {
		return rewardSmoothed, powerSmoothed, fmt.Errorf("failed to load power actor state: %v", err)
	} else if powerSmoothed, err = s.TotalPowerSmoothed(); err != nil {
		return rewardSmoothed, powerSmoothed, fmt.Errorf("failed to get total power: %v", err)
	}

	return rewardSmoothed, powerSmoothed, nil
}

func writeStrategyResults(filename string, results []StrategyResult) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"MinerID", "TerminationEpoch", "Status", "TotalSectors", "TerminateSectors", "ExpireSectors", "TerminationFee(FIL)", "ExpirationFee(FIL)", "TotalFee(FIL)", "Error"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write results
	for _, result := range results {
		record := []string{
			result.MinerID,
			fmt.Sprintf("%d", result.TerminationEpoch),
			result.Status,
			fmt.Sprintf("%d", result.TotalSectors),
			fmt.Sprintf("%d", result.TerminateSectors),
			fmt.Sprintf("%d", result.ExpireSectors),
			types.FIL(result.TerminationFee).String(),
			types.FIL(result.ExpirationFee).String(),
			types.FIL(result.TotalFee).String(),
			result.Error,
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}

func printStrategyResults(results []StrategyResult, verbose bool) {
	fmt.Printf("\n=== Strategy Results ===\n")
	fmt.Printf("%-12s %-10s %-8s %-6s %-6s %-6s %-25s %-25s %-25s %s\n",
		"MinerID", "Epoch", "Status", "Total", "Term.", "Exp.", "TermFee(FIL)", "ExpFee(FIL)", "TotalFee(FIL)", "Error")
	fmt.Println(strings.Repeat("-", 150))

	for _, result := range results {
		errorMsg := result.Error
		if len(errorMsg) > 20 {
			errorMsg = errorMsg[:20] + "..."
		}

		fmt.Printf("%-12s %-10d %-8s %-6d %-6d %-6d %-25s %-25s %-25s %s\n",
			result.MinerID,
			result.TerminationEpoch,
			result.Status,
			result.TotalSectors,
			result.TerminateSectors,
			result.ExpireSectors,
			types.FIL(result.TerminationFee).String(),
			types.FIL(result.ExpirationFee).String(),
			types.FIL(result.TotalFee).String(),
			errorMsg,
		)

		if verbose && result.Status == "success" && len(result.SectorDetails) > 0 {
			fmt.Printf("  Sector breakdown:\n")
			for i, detail := range result.SectorDetails {
				if i >= 10 && !verbose { // Limit output unless very verbose
					fmt.Printf("  ... and %d more sectors\n", len(result.SectorDetails)-i)
					break
				}
				fmt.Printf("    %d: %s (%.1fd) -> %s\n",
					detail.SectorNumber, detail.Strategy, detail.RemainingDays,
					types.FIL(detail.RecommendedFee))
			}
		}
	}
}

func printSummary(results []StrategyResult, totalTerminationFee, totalExpirationFee big.Int) {
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total miners processed: %d\n", len(results))

	successCount := 0
	totalSectors := 0
	totalTerminate := 0
	totalExpire := 0

	for _, r := range results {
		if r.Error == "" {
			successCount++
			totalSectors += r.TotalSectors
			totalTerminate += r.TerminateSectors
			totalExpire += r.ExpireSectors
		}
	}

	fmt.Printf("Successful calculations: %d\n", successCount)
	fmt.Printf("Failed calculations: %d\n", len(results)-successCount)
	fmt.Printf("Total sectors analyzed: %d\n", totalSectors)
	fmt.Printf("Sectors to terminate: %d (%.1f%%)\n", totalTerminate,
		float64(totalTerminate)*100/float64(totalSectors))
	fmt.Printf("Sectors to let expire: %d (%.1f%%)\n", totalExpire,
		float64(totalExpire)*100/float64(totalSectors))
	fmt.Printf("Total termination fees: %s FIL\n", types.FIL(totalTerminationFee).String())
	fmt.Printf("Total expiration fees: %s FIL\n", types.FIL(totalExpirationFee).String())
	fmt.Printf("Combined total fees: %s FIL\n", types.FIL(big.Add(totalTerminationFee, totalExpirationFee)).String())

	// Calculate what full termination would cost
	allTerminationFee := big.Zero()
	for _, r := range results {
		if r.Error == "" {
			for _, detail := range r.SectorDetails {
				allTerminationFee = big.Add(allTerminationFee, detail.TerminationFee)
			}
		}
	}

	if totalSectors > 0 && allTerminationFee.GreaterThan(big.Zero()) {
		savings := big.Sub(allTerminationFee, big.Add(totalTerminationFee, totalExpirationFee))
		if savings.GreaterThan(big.Zero()) {
			fmt.Printf("Strategy savings vs full termination: %s FIL (%.2f%%)\n",
				types.FIL(savings).String(),
				float64(savings.Int64())*100/float64(allTerminationFee.Int64()))
		}
	}
}
