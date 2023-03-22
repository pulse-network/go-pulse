// Package pulse implements the PulseChain fork
package pulse

import (
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/params"
)

// Apply PrimordialPulse fork changes
func PrimordialPulseFork(state *state.StateDB, treasury *params.Treasury) {
	applySacrificeCredits(state, treasury)
	replaceDepositContract(state)
}
