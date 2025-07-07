package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
)

// EpochDuration is the duration of each epoch (30 seconds)
const EpochDuration = 30 * time.Second

// ParseSectorNumbers parses sector number string and returns slice of sector numbers
func ParseSectorNumbers(sectorsStr string) ([]abi.SectorNumber, error) {
	parts := strings.Split(sectorsStr, ",")
	sectorNumbers := make([]abi.SectorNumber, 0)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			// Range format: 1-5
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}

			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid start sector number: %s", rangeParts[0])
			}

			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid end sector number: %s", rangeParts[1])
			}

			if start > end {
				return nil, fmt.Errorf("start sector number cannot be greater than end sector number: %d > %d", start, end)
			}

			for i := start; i <= end; i++ {
				sectorNumbers = append(sectorNumbers, abi.SectorNumber(i))
			}
		} else {
			// Single sector number
			num, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid sector number: %s", part)
			}
			if num < 0 {
				return nil, fmt.Errorf("sector number cannot be negative: %d", num)
			}
			sectorNumbers = append(sectorNumbers, abi.SectorNumber(num))
		}
	}

	return sectorNumbers, nil
}

// AdjustNetworkParams adjusts network parameters for future estimation
func AdjustNetworkParams(reward, power builtin.FilterEstimate, projectionEpochs abi.ChainEpoch) (builtin.FilterEstimate, builtin.FilterEstimate) {
	if projectionEpochs <= 0 {
		return reward, power
	}

	// Default estimation parameters based on reasonable assumptions
	powerGrowthRate := 0.0001  // ~0.01% growth per epoch
	rewardDecayRate := 0.00005 // ~0.005% decay per epoch

	// Apply growth rates
	growthFactor := 1.0 + powerGrowthRate*float64(projectionEpochs)
	decayFactor := 1.0 - rewardDecayRate*float64(projectionEpochs)

	if decayFactor < 0.1 {
		decayFactor = 0.1 // minimum 10%
	}

	// Adjust power
	newPowerPos := big.Mul(power.PositionEstimate, big.NewInt(int64(growthFactor*1000)))
	newPowerPos = big.Div(newPowerPos, big.NewInt(1000))
	newPowerVel := big.Mul(power.VelocityEstimate, big.NewInt(int64(growthFactor*1000)))
	newPowerVel = big.Div(newPowerVel, big.NewInt(1000))

	// Adjust reward
	newRewardPos := big.Mul(reward.PositionEstimate, big.NewInt(int64(decayFactor*1000)))
	newRewardPos = big.Div(newRewardPos, big.NewInt(1000))
	newRewardVel := big.Mul(reward.VelocityEstimate, big.NewInt(int64(decayFactor*1000)))
	newRewardVel = big.Div(newRewardVel, big.NewInt(1000))

	return builtin.FilterEstimate{
			PositionEstimate: newRewardPos,
			VelocityEstimate: newRewardVel,
		}, builtin.FilterEstimate{
			PositionEstimate: newPowerPos,
			VelocityEstimate: newPowerVel,
		}
}

// EpochsToDays converts epochs to days (2880 epochs = 1 day)
func EpochsToDays(epochs abi.ChainEpoch) float64 {
	return float64(epochs) / 2880.0
}

// DaysToEpochs converts days to epochs (1 day = 2880 epochs)
func DaysToEpochs(days float64) abi.ChainEpoch {
	return abi.ChainEpoch(days * 2880.0)
}

// EpochToTime converts epoch to time
func EpochToTime(epoch abi.ChainEpoch, genesisTime time.Time) time.Time {
	return genesisTime.Add(time.Duration(epoch) * EpochDuration)
}

// TimeToEpoch converts time to epoch
func TimeToEpoch(t time.Time, genesisTime time.Time) abi.ChainEpoch {
	if t.Before(genesisTime) {
		return 0
	}
	duration := t.Sub(genesisTime)
	return abi.ChainEpoch(duration / EpochDuration)
}

// ParseTime parses time string in various formats
// Times without timezone information are parsed as local time
func ParseTime(timeStr string) (time.Time, error) {
	// Formats with timezone information - use time.Parse (preserves timezone)
	utcFormats := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05 MST",
	}

	for _, format := range utcFormats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		}
	}

	// Formats without timezone information - use local time
	localFormats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"01/02/2006 15:04:05",
		"01/02/2006",
	}

	for _, format := range localFormats {
		if t, err := time.ParseInLocation(format, timeStr, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}
