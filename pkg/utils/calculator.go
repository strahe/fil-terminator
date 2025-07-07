package utils

import (
	"context"
	"fmt"

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
	cbor "github.com/ipfs/go-ipld-cbor"
)

type CalculationRequest struct {
	MinerID       string
	TargetEpoch   abi.ChainEpoch
	SectorNumbers []abi.SectorNumber // empty means all sectors
}

type SectorResult struct {
	SectorNumber abi.SectorNumber
	Fee          big.Int
	Age          abi.ChainEpoch
	IsExpired    bool
	ExpiredDays  float64
}

type CalculationResult struct {
	MinerID        string
	TargetEpoch    abi.ChainEpoch
	CurrentEpoch   abi.ChainEpoch
	IsEstimate     bool
	TotalSectors   int
	ActiveSectors  int
	ExpiredSectors int
	TotalFee       big.Int
	SectorResults  []SectorResult
	Error          string
}

func CalculateTerminationFee(ctx context.Context, api api.FullNode, req CalculationRequest) CalculationResult {
	result := CalculationResult{
		MinerID:     req.MinerID,
		TargetEpoch: req.TargetEpoch,
	}

	// Parse miner address
	mid, err := address.NewFromString(req.MinerID)
	if err != nil {
		result.Error = fmt.Sprintf("invalid miner address: %v", err)
		return result
	}

	bstore := blockstore.NewAPIBlockstore(api)
	adtStore := adt.WrapStore(ctx, cbor.NewCborStore(bstore))

	// Get current tipset
	currentTs, err := api.ChainHead(ctx)
	if err != nil {
		result.Error = fmt.Sprintf("failed to get current height: %v", err)
		return result
	}

	result.CurrentEpoch = currentTs.Height()

	// Determine target epoch
	if req.TargetEpoch == 0 {
		req.TargetEpoch = currentTs.Height()
	}
	result.TargetEpoch = req.TargetEpoch

	// Determine if estimation is needed
	result.IsEstimate = req.TargetEpoch > currentTs.Height()

	var ts *types.TipSet
	if result.IsEstimate {
		// Future estimation, use current data
		ts = currentTs
	} else {
		// Historical data, get actual tipset
		ts, err = api.ChainGetTipSetByHeight(ctx, req.TargetEpoch, types.EmptyTSK)
		if err != nil {
			result.Error = fmt.Sprintf("failed to get tipset at epoch %d: %v", req.TargetEpoch, err)
			return result
		}
	}

	nv, err := api.StateNetworkVersion(ctx, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get network version: %v", err)
		return result
	}

	minerAct, err := api.StateGetActor(ctx, mid, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get miner actor: %v", err)
		return result
	}

	minerInfo, err := api.StateMinerInfo(ctx, mid, ts.Key())
	if err != nil {
		result.Error = fmt.Sprintf("failed to get miner info: %v", err)
		return result
	}

	var minerVersion stactors.Version
	if _, version, ok := actors.GetActorMetaByCode(minerAct.Code); !ok {
		result.Error = fmt.Sprintf("unsupported miner actor code: %s", minerAct.Code)
		return result
	} else {
		minerVersion = version
	}

	if minerVersion < stactors.Version16 {
		result.Error = fmt.Sprintf("unsupported miner version %d", minerVersion)
		return result
	}

	// Get sectors
	var sectors []*miner.SectorOnChainInfo
	if len(req.SectorNumbers) == 0 {
		// Get all sectors
		sectors, err = api.StateMinerSectors(ctx, mid, nil, ts.Key())
		if err != nil {
			result.Error = fmt.Sprintf("failed to get sectors: %v", err)
			return result
		}
	} else {
		// Get specific sectors
		for _, num := range req.SectorNumbers {
			info, err := api.StateSectorGetInfo(ctx, mid, num, ts.Key())
			if err != nil {
				result.Error = fmt.Sprintf("failed to get sector %d info: %v", num, err)
				return result
			}
			sectors = append(sectors, info)
		}
	}

	result.TotalSectors = len(sectors)

	// Get network parameters
	var rewardSmoothed, powerSmoothed builtin.FilterEstimate

	if act, err := api.StateGetActor(ctx, reward.Address, ts.Key()); err != nil {
		result.Error = fmt.Sprintf("failed to load reward actor: %v", err)
		return result
	} else if s, err := reward.Load(adtStore, act); err != nil {
		result.Error = fmt.Sprintf("failed to load reward actor state: %v", err)
		return result
	} else if rewardSmoothed, err = s.ThisEpochRewardSmoothed(); err != nil {
		result.Error = fmt.Sprintf("failed to get smoothed reward: %v", err)
		return result
	}

	if act, err := api.StateGetActor(ctx, power.Address, ts.Key()); err != nil {
		result.Error = fmt.Sprintf("failed to load power actor: %v", err)
		return result
	} else if s, err := power.Load(adtStore, act); err != nil {
		result.Error = fmt.Sprintf("failed to load power actor state: %v", err)
		return result
	} else if powerSmoothed, err = s.TotalPowerSmoothed(); err != nil {
		result.Error = fmt.Sprintf("failed to get total power: %v", err)
		return result
	}

	// Calculate fees
	totalFee := big.Zero()
	expiredSectors := 0
	sectorResults := make([]SectorResult, 0, len(sectors))

	for _, sector := range sectors {
		sectorResult := SectorResult{
			SectorNumber: sector.SectorNumber,
		}

		// Check if sector has expired at target epoch
		if req.TargetEpoch >= sector.Expiration {
			expiredSectors++
			sectorResult.IsExpired = true
			sectorResult.ExpiredDays = EpochsToDays(req.TargetEpoch - sector.Expiration)
			sectorResults = append(sectorResults, sectorResult)
			continue
		}

		// Calculate sector age
		sectorAge := req.TargetEpoch - sector.Activation
		sectorResult.Age = sectorAge

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
			result.Error = fmt.Sprintf("failed to calculate fault fee: %v", err)
			return result
		}

		fee, err := miner.PledgePenaltyForTermination(nv, sector.InitialPledge, sectorAge, faultFee)
		if err != nil {
			result.Error = fmt.Sprintf("failed to calculate termination fee: %v", err)
			return result
		}

		sectorResult.Fee = fee
		totalFee = big.Add(totalFee, fee)
		sectorResults = append(sectorResults, sectorResult)
	}

	result.ExpiredSectors = expiredSectors
	result.ActiveSectors = result.TotalSectors - expiredSectors
	result.TotalFee = totalFee
	result.SectorResults = sectorResults

	return result
}
