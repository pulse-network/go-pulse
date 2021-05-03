package systemcontracts

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

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
	case params.BSCGenesisHash:
		network = mainNet
	case params.ChapelGenesisHash:
		network = chapelNet
	case params.RialtoGenesisHash:
		network = rialtoNet
	default:
		network = defaultNet
	}

	logger := log.New("system-contract-upgrade", network)

	switch blockNumber {
	case config.PrimordialPulseBlock:
		configs, err := primordialPulseUpgrade(config)
		if err != nil {
			return err
		}
		applySystemContractUpgrade(&Upgrade{
			UpgradeName: "PrimordialPulse",
			Configs:     configs,
		}, blockNumber, statedb, logger)
	default:
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

	logger.Info(fmt.Sprintf("Apply upgrade %s at height %d", upgrade.UpgradeName, blockNumber.Int64()))
	for _, cfg := range upgrade.Configs {
		logger.Info(fmt.Sprintf("Upgrade contract %s to commit %s", cfg.ContractAddr.String(), cfg.CommitUrl))

		if cfg.BeforeUpgrade != nil {
			err := cfg.BeforeUpgrade(blockNumber, cfg.ContractAddr, statedb)
			if err != nil {
				panic(fmt.Errorf("contract address: %s, execute beforeUpgrade error: %s", cfg.ContractAddr.String(), err.Error()))
			}
		}

		newContractCode, err := hex.DecodeString(cfg.Code)
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
