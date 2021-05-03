package parlia

import (
	"errors"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// Fetches the initial validators from the parlia config for bootstrapping the
// authorization snapshot on a new PulseChain fork.
func (p *Parlia) initPulsors() ([]common.Address, error) {
	// get validators from parlia config
	if p.config.InitValidators == nil || len(*p.config.InitValidators) == 0 {
		return nil, errors.New("missing initValidators in parlia config")
	}

	validators := make([]common.Address, len(*p.config.InitValidators))
	for i, addr := range *p.config.InitValidators {
		validators[i] = common.HexToAddress(addr)
	}
	return validators, nil
}

// Returns the byte array of sorted validators for validator rotation on epoch.
// If PrimordialPulseBlock happens to fall on an epoch, validators will be taken
// from the snapshot instead of the system contracts, which won't yet be deployed.
func (p *Parlia) getEpochValidatorBytes(header *types.Header, snap *Snapshot) ([]byte, error) {
	var (
		validators []common.Address
		err        error
	)

	if header.Number == p.chainConfig.PrimordialPulseBlock {
		// already sorted ascending by address
		validators = snap.validators()
	} else {
		validators, err = p.getCurrentValidators(header.ParentHash)
		if err != nil {
			return nil, err
		}

		// sort contract validator by address
		sort.Sort(validatorsAscending(validators))
	}

	validatorsBytes := make([]byte, len(validators)*validatorBytesLength)
	for i, validator := range validators {
		copy(validatorsBytes[i*validatorBytesLength:], validator.Bytes())
	}
	return validatorsBytes, nil
}

// Performs the initial allocations and balance adjustments for the PrimordialPulse fork.
func (p *Parlia) primordialPulseAlloctions(state *state.StateDB) error {
	log.Info("Applying PrimordialPulse fork allocations 💸")

	// state.SetBalance()

	return nil
}
