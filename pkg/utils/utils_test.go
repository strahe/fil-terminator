package utils

import (
	"fmt"
	"testing"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test genesis time for consistent testing
var TestGenesisTime = time.Date(2020, 8, 24, 22, 0, 0, 0, time.UTC)

func TestParseSectorNumbers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []abi.SectorNumber
		wantErr  bool
	}{
		{
			name:     "single sector",
			input:    "1",
			expected: []abi.SectorNumber{1},
			wantErr:  false,
		},
		{
			name:     "multiple sectors",
			input:    "1,2,3",
			expected: []abi.SectorNumber{1, 2, 3},
			wantErr:  false,
		},
		{
			name:     "range sectors",
			input:    "1-3",
			expected: []abi.SectorNumber{1, 2, 3},
			wantErr:  false,
		},
		{
			name:     "mixed format",
			input:    "1,3-5,7",
			expected: []abi.SectorNumber{1, 3, 4, 5, 7},
			wantErr:  false,
		},
		{
			name:     "range with spaces",
			input:    "1 - 3",
			expected: []abi.SectorNumber{1, 2, 3},
			wantErr:  false,
		},
		{
			name:     "sectors with spaces",
			input:    "1, 2 , 3",
			expected: []abi.SectorNumber{1, 2, 3},
			wantErr:  false,
		},
		{
			name:     "empty input",
			input:    "",
			expected: []abi.SectorNumber{},
			wantErr:  false,
		},
		{
			name:     "empty parts",
			input:    "1,,3",
			expected: []abi.SectorNumber{1, 3},
			wantErr:  false,
		},
		{
			name:     "zero sector",
			input:    "0",
			expected: []abi.SectorNumber{0},
			wantErr:  false,
		},
		{
			name:     "range starting with zero",
			input:    "0-2",
			expected: []abi.SectorNumber{0, 1, 2},
			wantErr:  false,
		},
		{
			name:     "single number range",
			input:    "5-5",
			expected: []abi.SectorNumber{5},
			wantErr:  false,
		},
		{
			name:     "large range",
			input:    "1-5",
			expected: []abi.SectorNumber{1, 2, 3, 4, 5},
			wantErr:  false,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: []abi.SectorNumber{},
			wantErr:  false,
		},
		{
			name:     "trailing comma",
			input:    "1,2,",
			expected: []abi.SectorNumber{1, 2},
			wantErr:  false,
		},
		{
			name:     "leading comma",
			input:    ",1,2",
			expected: []abi.SectorNumber{1, 2},
			wantErr:  false,
		},
		{
			name:     "multiple commas",
			input:    "1,,,2",
			expected: []abi.SectorNumber{1, 2},
			wantErr:  false,
		},
		{
			name:     "invalid sector number",
			input:    "abc",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "negative sector number",
			input:    "-1",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "invalid range format",
			input:    "1-2-3",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "invalid range start",
			input:    "abc-3",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "invalid range end",
			input:    "1-abc",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "reverse range",
			input:    "3-1",
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSectorNumbers(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestAdjustNetworkParams(t *testing.T) {
	// Create test filter estimates
	initialReward := builtin.FilterEstimate{
		PositionEstimate: big.NewInt(1000),
		VelocityEstimate: big.NewInt(100),
	}
	initialPower := builtin.FilterEstimate{
		PositionEstimate: big.NewInt(2000),
		VelocityEstimate: big.NewInt(200),
	}

	tests := []struct {
		name              string
		projectionEpochs  abi.ChainEpoch
		expectNoChange    bool
		expectRewardLower bool
		expectPowerHigher bool
	}{
		{
			name:             "no projection",
			projectionEpochs: 0,
			expectNoChange:   true,
		},
		{
			name:             "negative projection",
			projectionEpochs: -100,
			expectNoChange:   true,
		},
		{
			name:              "short term projection",
			projectionEpochs:  2880, // 1 day
			expectNoChange:    false,
			expectRewardLower: true,
			expectPowerHigher: true,
		},
		{
			name:              "medium term projection",
			projectionEpochs:  2880 * 30, // 30 days
			expectNoChange:    false,
			expectRewardLower: true,
			expectPowerHigher: true,
		},
		{
			name:              "long term projection",
			projectionEpochs:  2880 * 365, // 1 year
			expectNoChange:    false,
			expectRewardLower: true,
			expectPowerHigher: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adjustedReward, adjustedPower := AdjustNetworkParams(initialReward, initialPower, tt.projectionEpochs)

			if tt.expectNoChange {
				assert.Equal(t, initialReward.PositionEstimate, adjustedReward.PositionEstimate)
				assert.Equal(t, initialReward.VelocityEstimate, adjustedReward.VelocityEstimate)
				assert.Equal(t, initialPower.PositionEstimate, adjustedPower.PositionEstimate)
				assert.Equal(t, initialPower.VelocityEstimate, adjustedPower.VelocityEstimate)
			} else {
				if tt.expectRewardLower {
					assert.True(t, adjustedReward.PositionEstimate.LessThan(initialReward.PositionEstimate),
						"Expected reward to be lower")
					assert.True(t, adjustedReward.VelocityEstimate.LessThan(initialReward.VelocityEstimate),
						"Expected reward velocity to be lower")
				}
				if tt.expectPowerHigher {
					assert.True(t, adjustedPower.PositionEstimate.GreaterThan(initialPower.PositionEstimate),
						"Expected power to be higher")
					assert.True(t, adjustedPower.VelocityEstimate.GreaterThan(initialPower.VelocityEstimate),
						"Expected power velocity to be higher")
				}
			}
		})
	}
}

func TestAdjustNetworkParamsMinimumReward(t *testing.T) {
	// Test that reward decay doesn't go below minimum threshold
	initialReward := builtin.FilterEstimate{
		PositionEstimate: big.NewInt(1000),
		VelocityEstimate: big.NewInt(100),
	}
	initialPower := builtin.FilterEstimate{
		PositionEstimate: big.NewInt(2000),
		VelocityEstimate: big.NewInt(200),
	}

	// Very long projection that would normally result in near-zero reward
	veryLongProjection := abi.ChainEpoch(2880 * 365 * 10) // 10 years

	adjustedReward, _ := AdjustNetworkParams(initialReward, initialPower, veryLongProjection)

	// Check that reward hasn't gone below 10% of original
	minExpectedReward := big.Div(initialReward.PositionEstimate, big.NewInt(10))
	assert.True(t, adjustedReward.PositionEstimate.GreaterThanEqual(minExpectedReward),
		"Reward should not go below 10% of original value")
}

func TestEpochsToDays(t *testing.T) {
	tests := []struct {
		name     string
		epochs   abi.ChainEpoch
		expected float64
	}{
		{
			name:     "one day",
			epochs:   2880,
			expected: 1.0,
		},
		{
			name:     "half day",
			epochs:   1440,
			expected: 0.5,
		},
		{
			name:     "one week",
			epochs:   2880 * 7,
			expected: 7.0,
		},
		{
			name:     "zero epochs",
			epochs:   0,
			expected: 0.0,
		},
		{
			name:     "negative epochs",
			epochs:   -2880,
			expected: -1.0,
		},
		{
			name:     "fractional day",
			epochs:   720, // 0.25 days
			expected: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EpochsToDays(tt.epochs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDaysToEpochs(t *testing.T) {
	tests := []struct {
		name     string
		days     float64
		expected abi.ChainEpoch
	}{
		{
			name:     "one day",
			days:     1.0,
			expected: 2880,
		},
		{
			name:     "half day",
			days:     0.5,
			expected: 1440,
		},
		{
			name:     "one week",
			days:     7.0,
			expected: 2880 * 7,
		},
		{
			name:     "zero days",
			days:     0.0,
			expected: 0,
		},
		{
			name:     "negative days",
			days:     -1.0,
			expected: -2880,
		},
		{
			name:     "fractional day",
			days:     0.25,
			expected: 720,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysToEpochs(tt.days)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEpochDayConversion(t *testing.T) {
	// Test that conversion functions are inverse of each other
	testEpochs := []abi.ChainEpoch{0, 1440, 2880, 5760, 2880 * 30, 2880 * 365}

	for _, epochs := range testEpochs {
		t.Run(fmt.Sprintf("epochs_%d", epochs), func(t *testing.T) {
			days := EpochsToDays(epochs)
			backToEpochs := DaysToEpochs(days)
			assert.Equal(t, epochs, backToEpochs)
		})
	}

	testDays := []float64{0.0, 0.5, 1.0, 2.0, 30.0, 365.0}

	for _, days := range testDays {
		t.Run(fmt.Sprintf("days_%.1f", days), func(t *testing.T) {
			epochs := DaysToEpochs(days)
			backToDays := EpochsToDays(epochs)
			assert.Equal(t, days, backToDays)
		})
	}
}

func TestEpochToTime(t *testing.T) {
	tests := []struct {
		name     string
		epoch    abi.ChainEpoch
		expected time.Time
	}{
		{
			name:     "genesis epoch",
			epoch:    0,
			expected: TestGenesisTime,
		},
		{
			name:     "epoch 1",
			epoch:    1,
			expected: TestGenesisTime.Add(30 * time.Second),
		},
		{
			name:     "one hour",
			epoch:    120, // 120 epochs * 30 seconds = 3600 seconds = 1 hour
			expected: TestGenesisTime.Add(1 * time.Hour),
		},
		{
			name:     "one day",
			epoch:    2880, // 2880 epochs * 30 seconds = 86400 seconds = 1 day
			expected: TestGenesisTime.Add(24 * time.Hour),
		},
		{
			name:     "negative epoch",
			epoch:    -60, // -60 epochs * 30 seconds = -1800 seconds = -30 minutes
			expected: TestGenesisTime.Add(-30 * time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EpochToTime(tt.epoch, TestGenesisTime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTimeToEpoch(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected abi.ChainEpoch
	}{
		{
			name:     "genesis time",
			time:     TestGenesisTime,
			expected: 0,
		},
		{
			name:     "30 seconds after genesis",
			time:     TestGenesisTime.Add(30 * time.Second),
			expected: 1,
		},
		{
			name:     "one hour after genesis",
			time:     TestGenesisTime.Add(1 * time.Hour),
			expected: 120,
		},
		{
			name:     "one day after genesis",
			time:     TestGenesisTime.Add(24 * time.Hour),
			expected: 2880,
		},
		{
			name:     "before genesis",
			time:     TestGenesisTime.Add(-1 * time.Hour),
			expected: 0,
		},
		{
			name:     "fractional epoch",
			time:     TestGenesisTime.Add(45 * time.Second), // 1.5 epochs, should truncate to 1
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TimeToEpoch(tt.time, TestGenesisTime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name     string
		timeStr  string
		wantErr  bool
		expected time.Time
	}{
		{
			name:     "standard format",
			timeStr:  "2024-01-01 12:00:00",
			wantErr:  false,
			expected: time.Date(2024, 1, 1, 12, 0, 0, 0, time.Local),
		},
		{
			name:     "ISO format",
			timeStr:  "2024-01-01T12:00:00",
			wantErr:  false,
			expected: time.Date(2024, 1, 1, 12, 0, 0, 0, time.Local),
		},
		{
			name:     "ISO format with Z",
			timeStr:  "2024-01-01T12:00:00Z",
			wantErr:  false,
			expected: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:     "date only",
			timeStr:  "2024-01-01",
			wantErr:  false,
			expected: time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),
		},
		{
			name:     "US format",
			timeStr:  "01/01/2024 12:00:00",
			wantErr:  false,
			expected: time.Date(2024, 1, 1, 12, 0, 0, 0, time.Local),
		},
		{
			name:     "US date only",
			timeStr:  "01/01/2024",
			wantErr:  false,
			expected: time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),
		},
		{
			name:     "invalid format",
			timeStr:  "invalid time",
			wantErr:  true,
			expected: time.Time{},
		},
		{
			name:     "empty string",
			timeStr:  "",
			wantErr:  true,
			expected: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTime(tt.timeStr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTimeConversion(t *testing.T) {
	// Test that EpochToTime and TimeToEpoch are inverse functions
	testEpochs := []abi.ChainEpoch{0, 1, 120, 2880, 2880 * 30, 2880 * 365}

	for _, epoch := range testEpochs {
		t.Run(fmt.Sprintf("epoch_%d", epoch), func(t *testing.T) {
			time := EpochToTime(epoch, TestGenesisTime)
			backToEpoch := TimeToEpoch(time, TestGenesisTime)
			assert.Equal(t, epoch, backToEpoch)
		})
	}

	// Test with specific times
	testTimes := []time.Time{
		TestGenesisTime,
		TestGenesisTime.Add(1 * time.Hour),
		TestGenesisTime.Add(24 * time.Hour),
		TestGenesisTime.Add(30 * 24 * time.Hour),
		TestGenesisTime.Add(365 * 24 * time.Hour),
	}

	for _, testTime := range testTimes {
		t.Run(fmt.Sprintf("time_%s", testTime.Format("2006-01-02")), func(t *testing.T) {
			epoch := TimeToEpoch(testTime, TestGenesisTime)
			backToTime := EpochToTime(epoch, TestGenesisTime)
			// Due to epoch truncation, we need to check if the time is within the epoch duration
			diff := testTime.Sub(backToTime)
			assert.True(t, diff >= 0 && diff < EpochDuration, "Time should be within epoch duration")
		})
	}
}

func BenchmarkParseSectorNumbers(b *testing.B) {
	testInputs := []string{
		"1",
		"1,2,3,4,5",
		"1-100",
		"1-10,20-30,40-50",
		"1,3,5,7,9,11,13,15,17,19",
	}

	for _, input := range testInputs {
		b.Run(input, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err := ParseSectorNumbers(input)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkAdjustNetworkParams(b *testing.B) {
	reward := builtin.FilterEstimate{
		PositionEstimate: big.NewInt(1000000),
		VelocityEstimate: big.NewInt(100000),
	}
	power := builtin.FilterEstimate{
		PositionEstimate: big.NewInt(2000000),
		VelocityEstimate: big.NewInt(200000),
	}

	projections := []abi.ChainEpoch{
		2880,       // 1 day
		2880 * 7,   // 1 week
		2880 * 30,  // 1 month
		2880 * 365, // 1 year
	}

	for _, projection := range projections {
		b.Run(fmt.Sprintf("projection_%d", projection), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				AdjustNetworkParams(reward, power, projection)
			}
		})
	}
}

func BenchmarkEpochsToDays(b *testing.B) {
	testEpochs := []abi.ChainEpoch{2880, 2880 * 7, 2880 * 30, 2880 * 365}

	for _, epochs := range testEpochs {
		b.Run(fmt.Sprintf("epochs_%d", epochs), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				EpochsToDays(epochs)
			}
		})
	}
}

func BenchmarkDaysToEpochs(b *testing.B) {
	testDays := []float64{1.0, 7.0, 30.0, 365.0}

	for _, days := range testDays {
		b.Run(fmt.Sprintf("days_%.1f", days), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				DaysToEpochs(days)
			}
		})
	}
}

func BenchmarkEpochToTime(b *testing.B) {
	testEpochs := []abi.ChainEpoch{0, 2880, 2880 * 30, 2880 * 365}

	for _, epoch := range testEpochs {
		b.Run(fmt.Sprintf("epoch_%d", epoch), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				EpochToTime(epoch, TestGenesisTime)
			}
		})
	}
}

func BenchmarkTimeToEpoch(b *testing.B) {
	testTimes := []time.Time{
		TestGenesisTime,
		TestGenesisTime.Add(24 * time.Hour),
		TestGenesisTime.Add(30 * 24 * time.Hour),
		TestGenesisTime.Add(365 * 24 * time.Hour),
	}

	for _, testTime := range testTimes {
		b.Run(fmt.Sprintf("time_%s", testTime.Format("2006-01-02")), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				TimeToEpoch(testTime, TestGenesisTime)
			}
		})
	}
}

func BenchmarkParseTime(b *testing.B) {
	testTimeStrings := []string{
		"2024-01-01 12:00:00",
		"2024-01-01T12:00:00",
		"2024-01-01T12:00:00Z",
		"2024-01-01",
		"01/01/2024 12:00:00",
	}

	for _, timeStr := range testTimeStrings {
		b.Run(timeStr, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ParseTime(timeStr)
			}
		})
	}
}
