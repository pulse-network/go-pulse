package systemcontracts

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

type UpgradeConfig struct {
	BeforeUpgrade upgradeHook
	AfterUpgrade  upgradeHook
	ContractAddr  common.Address
	CommitUrl     string
	Code          string
}

type Upgrade struct {
	UpgradeName string
	Configs     []*UpgradeConfig
}

type upgradeHook func(blockNumber *big.Int, contractAddr common.Address, statedb *state.StateDB) error

const (
	mainNet    = "Mainnet"
	chapelNet  = "Chapel"
	rialtoNet  = "Rialto"
	defaultNet = "Default"
)

var (
	GenesisHash common.Hash
)

func init() {
	// reserved for future use to instantiate Upgrade vars
}

func UpgradeBuildInSystemContract(config *params.ChainConfig, blockNumber *big.Int, statedb *state.StateDB) error {
	if config == nil || blockNumber == nil || statedb == nil {
		return nil
	}
	var network string
	switch GenesisHash {
	/* Add mainnet genesis hash */
	case params.GoerliGenesisHash:
		network = mainNet
	default:
		network = defaultNet
	}

	logger := log.New("system-contract-upgrade", network)

	if config.IsPrimordialPulseBlock(blockNumber.Uint64()) {
		configs, err := primordialPulseUpgrade(config)
		if err != nil {
			return err
		}
		applySystemContractUpgrade(&Upgrade{
			UpgradeName: "PrimordialPulse",
			Configs:     configs,
		}, blockNumber, statedb, logger)

		// reset system contract balances to 0, in case of carry-over funds from ETH state
		for _, cfg := range configs {
			logger.Info(fmt.Sprintf("Resetting contract %s balance to 0", cfg.ContractAddr.String()))
			statedb.SetBalance(cfg.ContractAddr, big.NewInt(0))
		}
	} else {
		logger.Debug("No system contract updates to apply", "height", blockNumber.String())
	}

	return nil
}

func primordialPulseUpgrade(config *params.ChainConfig) ([]*UpgradeConfig, error) {
	if config.Parlia.SystemContracts == nil {
		return nil, errors.New("Missing systemContracts in parlia config for PrimordialPulse fork")
	}

	upgrades := make([]*UpgradeConfig, len(*config.Parlia.SystemContracts))
	for i, contract := range *config.Parlia.SystemContracts {
		upgrades[i] = &UpgradeConfig{
			ContractAddr: common.HexToAddress(contract.Addr),
			Code:         contract.Code,
		}
	}

	return upgrades, nil
}

func applySystemContractUpgrade(upgrade *Upgrade, blockNumber *big.Int, statedb *state.StateDB, logger log.Logger) {
	if upgrade == nil {
		logger.Info("Empty upgrade config", "height", blockNumber.String())
		return
	}

	logger.Info(fmt.Sprintf("Applying upgrade %s at height %d", upgrade.UpgradeName, blockNumber.Int64()))
	for _, cfg := range upgrade.Configs {
		logger.Info(fmt.Sprintf("Upgrade contract %s to commit %s", cfg.ContractAddr.String(), cfg.CommitUrl))

		if cfg.BeforeUpgrade != nil {
			err := cfg.BeforeUpgrade(blockNumber, cfg.ContractAddr, statedb)
			if err != nil {
				panic(fmt.Errorf("contract address: %s, execute beforeUpgrade error: %s", cfg.ContractAddr.String(), err.Error()))
			}
		}

		newContractCode, err := hex.DecodeString(strings.TrimPrefix(cfg.Code, "0x"))
		if err != nil {
			panic(fmt.Errorf("failed to decode new contract code: %s", err.Error()))
		}
		statedb.SetCode(cfg.ContractAddr, newContractCode)

		if cfg.AfterUpgrade != nil {
			err := cfg.AfterUpgrade(blockNumber, cfg.ContractAddr, statedb)
			if err != nil {
				panic(fmt.Errorf("contract address: %s, execute afterUpgrade error: %s", cfg.ContractAddr.String(), err.Error()))
			}
		}
	}
}
