package params

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

// Optional treasury for launching PulseChain testnets
type Treasury struct {
	Addr    string                `json:"addr"`
	Balance *math.HexOrDecimal256 `json:"balance"`
}

// A trivially small amount of work to add to the Ethereum Mainnet TTD
// to allow for un-merging and merging with the PulseChain beacon chain
var PulseChainTTDOffset = big.NewInt(131_072)

// This value is defined as LAST_ACTUAL_MAINNET_TTD + PulseChainTTDOffset
// where LAST_ACTUAL_MAINNET_TTD = 58_750_003_716_598_352_816_469
var PulseChainTerminalTotalDifficulty, _ = new(big.Int).SetString("58_750_003_716_598_352_947_541", 0)

var (
	PulseChainNetworkId = uint64(369)

	// PulseChainTrustedCheckpoint contains the light client trusted checkpoint for the main network.
	PulseChainTrustedCheckpoint = &TrustedCheckpoint{
		SectionIndex: 506,
		SectionHead:  common.HexToHash("0x3d1a139a6fc7764211236ef7c64d9e8c1fe55b358d7414e25277bac1144486cd"),
		CHTRoot:      common.HexToHash("0xef7fc3321a239a54238593bdf68d82933d903cb533b0d03228a8d958cd35ea77"),
		BloomRoot:    common.HexToHash("0x51d7bfe7c6397b1caa8b1cb046de4aeaf7e7fbd3fb6c726b60bf750de78809e8"),
	}

	PulseChainConfig = &ChainConfig{
		ChainID:                       big.NewInt(369),
		HomesteadBlock:                big.NewInt(1_150_000),
		DAOForkBlock:                  big.NewInt(1_920_000),
		DAOForkSupport:                true,
		EIP150Block:                   big.NewInt(2_463_000),
		EIP155Block:                   big.NewInt(2_675_000),
		EIP158Block:                   big.NewInt(2_675_000),
		ByzantiumBlock:                big.NewInt(4_370_000),
		ConstantinopleBlock:           big.NewInt(7_280_000),
		PetersburgBlock:               big.NewInt(7_280_000),
		IstanbulBlock:                 big.NewInt(9_069_000),
		MuirGlacierBlock:              big.NewInt(9_200_000),
		BerlinBlock:                   big.NewInt(12_244_000),
		LondonBlock:                   big.NewInt(12_965_000),
		ArrowGlacierBlock:             big.NewInt(13_773_000),
		GrayGlacierBlock:              big.NewInt(15_050_000),
		TerminalTotalDifficulty:       PulseChainTerminalTotalDifficulty,
		TerminalTotalDifficultyPassed: true,
		Ethash:                        new(EthashConfig),
		PrimordialPulseBlock:          big.NewInt(17_233_000),
		ShanghaiTime:                  newUint64(1683786515),
	}

	PulseChainTestnetV4NetworkId = uint64(943)

	// PulseChainTestnetV4TrustedCheckpoint contains the light client trusted checkpoint for the test network.
	PulseChainTestnetV4TrustedCheckpoint = &TrustedCheckpoint{
		SectionIndex: 451,
		SectionHead:  common.HexToHash("0xe47f84b9967eb2ad2afff74d59901b63134660011822fdababaf8fdd18a75aa6"),
		CHTRoot:      common.HexToHash("0xc31e0462ca3d39a46111bb6b63ac4e1cac84089472b7474a319d582f72b3f0c0"),
		BloomRoot:    common.HexToHash("0x7c9f25ce3577a3ab330d52a7343f801899cf9d4980c69f81de31ccc1a055c809"),
	}

	PulseChainTestnetV4Config = &ChainConfig{
		ChainID:                       big.NewInt(943),
		HomesteadBlock:                big.NewInt(1_150_000),
		DAOForkBlock:                  big.NewInt(1_920_000),
		DAOForkSupport:                true,
		EIP150Block:                   big.NewInt(2_463_000),
		EIP155Block:                   big.NewInt(2_675_000),
		EIP158Block:                   big.NewInt(2_675_000),
		ByzantiumBlock:                big.NewInt(4_370_000),
		ConstantinopleBlock:           big.NewInt(7_280_000),
		PetersburgBlock:               big.NewInt(7_280_000),
		IstanbulBlock:                 big.NewInt(9_069_000),
		MuirGlacierBlock:              big.NewInt(9_200_000),
		BerlinBlock:                   big.NewInt(12_244_000),
		LondonBlock:                   big.NewInt(12_965_000),
		ArrowGlacierBlock:             big.NewInt(13_773_000),
		GrayGlacierBlock:              big.NewInt(15_050_000),
		TerminalTotalDifficulty:       PulseChainTerminalTotalDifficulty,
		TerminalTotalDifficultyPassed: true,
		Ethash:                        new(EthashConfig),
		PrimordialPulseBlock:          big.NewInt(16_492_700),
		Treasury:                      testnetTreasury(),
		ShanghaiTime:                  newUint64(1682700369),
	}
)

func testnetTreasury() *Treasury {
	var pulseChainTestnetTreasuryBalance math.HexOrDecimal256
	pulseChainTestnetTreasuryBalance.UnmarshalText([]byte("0x314DC6448D9338C15B0A00000000"))

	return &Treasury{
		Addr:    "0xA592ED65885bcbCeb30442F4902a0D1Cf3AcB8fC",
		Balance: &pulseChainTestnetTreasuryBalance,
	}
}
