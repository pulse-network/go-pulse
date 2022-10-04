package pulse

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

// Applies the sacrifice credits for the PrimordialPulse fork.
func TestApplySacrificeCredits(t *testing.T) {
	// Init
	var pulseChainTestnetTreasuryBalance math.HexOrDecimal256
	pulseChainTestnetTreasuryBalance.UnmarshalText([]byte("0xC9F2C9CD04674EDEA40000000"))

	db := rawdb.NewMemoryDatabase()
	state, _ := state.New(common.Hash{}, state.NewDatabaseWithConfig(db, &trie.Config{Preimages: true}), nil)
	treasury := &params.Treasury{
		Addr:    "0xceB59257450820132aB274ED61C49E5FD96E8868",
		Balance: &pulseChainTestnetTreasuryBalance,
	}

	// Exec
	ApplySacrificeCredits(state, treasury)

	// Verify
	actual := state.GetBalance(common.HexToAddress(treasury.Addr))
	expected := (*big.Int)(treasury.Balance)
	if actual.Cmp(expected) != 0 {
		t.Errorf("Invalid treasury balance, actual: %d, expected: %d", actual, expected)
	} else {
		t.Log("Treasury allocating successful")
	}

	// from the credits.csv file in compressed-allocations
	actual = state.GetBalance(common.HexToAddress("0x0000000000000000000000000000000000001010"))
	expected, _ = new(big.Int).SetString("5977597164464952199640526", 10)

	if actual.Cmp(expected) != 0 {
		t.Errorf("Invalid sacrifice credit balance, actual: %d, expected: %d", actual, expected)
	} else {
		t.Log("Sacrifice allocation successful")
	}
}