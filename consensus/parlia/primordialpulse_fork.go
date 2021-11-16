package parlia

import (
	_ "embed"
	"errors"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// see https://gitlab.com/pulsechaincom/compressed-allocations
//go:embed primordialpulse_credits.bin
var rawCredits []byte

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
// from the snapshot instead of the system contracts, which won't yet be deployed & initialized.
func (p *Parlia) getEpochValidatorBytes(header *types.Header, snap *Snapshot) ([]byte, error) {
	var (
		validators []common.Address
		err        error
	)

	if p.chainConfig.IsPrimordialPulseBlock(header.Number.Uint64()) {
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
func (p *Parlia) primordialPulseAlloctions(state *state.StateDB) {
	if p.config.Treasury != nil {
		log.Info("Applying PrimordialPulse treasury allocation 💸")
		state.AddBalance(common.HexToAddress(p.config.Treasury.Addr), (*big.Int)(p.config.Treasury.Balance))
	}

	log.Info("Awarding PrimordialPulse sacrifice credits 💸")
	for ptr := 0; ptr < len(rawCredits); {
		byteCount := int(rawCredits[ptr])
		ptr++

		record := rawCredits[ptr : ptr+byteCount]
		ptr += byteCount

		addr := common.BytesToAddress(record[:20])
		credit := new(big.Int).SetBytes(record[20:])
		state.AddBalance(addr, credit)
	}
}
