package parlia

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

func (p *Parlia) initializeValidators() ([]common.Address, error) {
	// get validators from parlia config
	if p.config.InitValidators == nil || len(p.config.InitValidators) == 0 {
		return nil, errors.New("missing initValidators in parlia config")
	}

	validators := make([]common.Address, len(p.config.InitValidators))
	for i, addr := range p.config.InitValidators {
		validators[i] = common.HexToAddress(addr)
	}
	return validators, nil
}
