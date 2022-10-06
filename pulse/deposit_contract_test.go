package pulse

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/trie"
)

func TestReplaceDepositContract(t *testing.T) {
	// Init
	db := rawdb.NewMemoryDatabase()
	state, _ := state.New(common.Hash{}, state.NewDatabaseWithConfig(db, &trie.Config{Preimages: true}), nil)

	// Exec
	replaceDepositContract(state)

	actualStorage := state.GetState(pulseDepositContractAddr, common.HexToHash(depositContractStorage[0][0]))
	expectedStorage := common.HexToHash(depositContractStorage[0][1])
	if actualStorage != expectedStorage {
		t.Errorf("Invalid storage entry, actual: %d, expected: %d", actualStorage, expectedStorage)
	} else {
		t.Log("Valid Storage entry")
	}
}
