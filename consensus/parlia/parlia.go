package parlia

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	inMemorySnapshots  = 128  // Number of recent snapshots to keep in memory
	inMemorySignatures = 4096 // Number of recent block signatures to keep in memory

	checkpointInterval = 1024        // Number of blocks after which to save the snapshot to the database
	defaultEpochLength = uint64(100) // Default number of blocks of checkpoint to update validatorSet from contract

	extraVanity      = 32 // Fixed number of extra-data prefix bytes reserved for signer vanity
	extraSeal        = 65 // Fixed number of extra-data suffix bytes reserved for signer seal
	nextForkHashSize = 4  // Fixed number of extra-data suffix bytes reserved for nextForkHash.

	validatorBytesLength = common.AddressLength
	wiggleTime           = uint64(1) // second, Random delay (per signer) to allow concurrent signers
	initialBackOffTime   = uint64(1) // second

	systemRewardPercent = 4 // it means 1/2^4 = 1/16 percentage of gas fee incoming will be distributed to system

)

var (
	uncleHash  = types.CalcUncleHash(nil) // Always Keccak256(RLP([])) as uncles are meaningless outside of PoW.
	diffInTurn = big.NewInt(2)            // Block difficulty for in-turn signatures
	diffNoTurn = big.NewInt(1)            // Block difficulty for out-of-turn signatures
	// 100 native token
	maxSystemBalance = new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether))

	systemContracts = map[common.Address]bool{
		common.HexToAddress(systemcontracts.ValidatorContract): true,
		common.HexToAddress(systemcontracts.SlashingContract):  true,
		common.HexToAddress(systemcontracts.StakingContract):   true,
	}
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	// errUnknownBlock is returned when the list of validators is requested for a block
	// that is not part of the local blockchain.
	errUnknownBlock = errors.New("unknown block")

	// errMissingVanity is returned if a block's extra-data section is shorter than
	// 32 bytes, which is required to store the signer vanity.
	errMissingVanity = errors.New("extra-data 32 byte vanity prefix missing")

	// errMissingSignature is returned if a block's extra-data section doesn't seem
	// to contain a 65 byte secp256k1 signature.
	errMissingSignature = errors.New("extra-data 65 byte signature suffix missing")

	// errExtraValidators is returned if non-sprint-end block contain validator data in
	// their extra-data fields.
	errExtraValidators = errors.New("non-sprint-end block contains extra validator list")

	// errInvalidSpanValidators is returned if a block contains an
	// invalid list of validators (i.e. non divisible by 20 bytes).
	errInvalidSpanValidators = errors.New("invalid validator list on sprint end block")

	// errInvalidMixDigest is returned if a block's mix digest is non-zero.
	errInvalidMixDigest = errors.New("non-zero mix digest")

	// errInvalidUncleHash is returned if a block contains an non-empty uncle list.
	errInvalidUncleHash = errors.New("non empty uncle hash")

	// errMismatchingEpochValidators is returned if a sprint block contains a
	// list of validators different than the one the local node calculated.
	errMismatchingEpochValidators = errors.New("mismatching validator list on epoch block")

	// errInvalidDifficulty is returned if the difficulty of a block is missing.
	errInvalidDifficulty = errors.New("invalid difficulty")

	// errWrongDifficulty is returned if the difficulty of a block doesn't match the
	// turn of the signer.
	errWrongDifficulty = errors.New("wrong difficulty")

	// errOutOfRangeChain is returned if an authorization list is attempted to
	// be modified via out-of-range or non-contiguous headers.
	errOutOfRangeChain = errors.New("out of range or non-contiguous chain")

	// errBlockHashInconsistent is returned if an authorization list is attempted to
	// insert an inconsistent block.
	errBlockHashInconsistent = errors.New("the block hash is inconsistent")

	// errCoinBaseMisMatch is returned if a header's coinbase do not match with signature
	errCoinBaseMisMatch = errors.New("coinbase do not match with signature")

	// errRecentlySigned is returned if a header is signed by an authorized entity
	// that already signed a header recently, thus is temporarily not allowed to.
	errRecentlySigned = errors.New("recently signed")
)

// SignerFn is a signer callback function to request a header to be signed by a
// backing account.
type SignerFn func(accounts.Account, string, []byte) ([]byte, error)
type SignerTxFn func(accounts.Account, *types.Transaction, *big.Int) (*types.Transaction, error)

func isToSystemContract(to common.Address) bool {
	return systemContracts[to]
}

// ecrecover extracts the Ethereum account address from a signed header.
func ecrecover(header *types.Header, sigCache *lru.ARCCache, chainId *big.Int) (common.Address, error) {
	// If the signature's already cached, return that
	hash := header.Hash()
	if address, known := sigCache.Get(hash); known {
		return address.(common.Address), nil
	}
	// Retrieve the signature from the header extra-data
	if len(header.Extra) < extraSeal {
		return common.Address{}, errMissingSignature
	}
	signature := header.Extra[len(header.Extra)-extraSeal:]

	// Recover the public key and the Ethereum address
	pubkey, err := crypto.Ecrecover(SealHash(header, chainId).Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])

	sigCache.Add(hash, signer)
	return signer, nil
}

// ParliaRLP returns the rlp bytes which needs to be signed for the parlia
// sealing. The RLP to sign consists of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.
func ParliaRLP(header *types.Header, chainId *big.Int) []byte {
	b := new(bytes.Buffer)
	encodeSigHeader(b, header, chainId)
	return b.Bytes()
}

// Parlia is the consensus engine of BSC
type Parlia struct {
	chainConfig     *params.ChainConfig  // Chain config
	config          *params.ParliaConfig // Consensus engine configuration parameters for parlia consensus
	genesisHash     common.Hash
	db              ethdb.Database // Database to store and retrieve snapshot checkpoints
	recentSnaps     *lru.ARCCache  // Snapshots for recent block to speed up
	signatures      *lru.ARCCache  // Signatures of recent blocks to speed up mining
	signer          types.Signer   // Ethereum address of the signing key
	val             common.Address // Ethereum address of the signing key
	signFn          SignerFn       // Signer function to authorize hashes with
	signTxFn        SignerTxFn     //Sign transaction function to sign tx
	lock            sync.RWMutex   // Protects the signer fields
	ethAPI          *ethapi.PublicBlockChainAPI
	validatorSetABI abi.ABI
	slashABI        abi.ABI
	stakingABI      abi.ABI

	// The fields below are for testing only
	fakeDiff bool // Skip difficulty verifications
	// Used for synchronizing pulse chain prior to the PrimordialPulseBlock
	// Will be lazily instantiated as needed
	makeEthash func() *ethash.Ethash
	_ethash    *ethash.Ethash // The lazily-loaded instance
}

// New creates a Parlia consensus engine.
func New(chainConfig *params.ChainConfig, db ethdb.Database, ethAPI *ethapi.PublicBlockChainAPI, genesisHash common.Hash, makeEthash func() *ethash.Ethash) *Parlia {
	// get parlia config
	parliaConfig := chainConfig.Parlia

	// Set any missing consensus parameters to their defaults
	if parliaConfig != nil && parliaConfig.Epoch == 0 {
		parliaConfig.Epoch = defaultEpochLength
	}

	// Allocate the snapshot caches and create the engine
	recentSnaps, err := lru.NewARC(inMemorySnapshots)
	if err != nil {
		panic(err)
	}
	signatures, err := lru.NewARC(inMemorySignatures)
	if err != nil {
		panic(err)
	}
	vABI, err := abi.JSON(strings.NewReader(validatorSetABI))
	if err != nil {
		panic(err)
	}
	slABI, err := abi.JSON(strings.NewReader(slashingABI))
	if err != nil {
		panic(err)
	}
	stABI, err := abi.JSON(strings.NewReader(stakingABI))
	if err != nil {
		panic(err)
	}

	return &Parlia{
		chainConfig:     chainConfig,
		config:          parliaConfig,
		genesisHash:     genesisHash,
		db:              db,
		ethAPI:          ethAPI,
		recentSnaps:     recentSnaps,
		signatures:      signatures,
		validatorSetABI: vABI,
		slashABI:        slABI,
		stakingABI:      stABI,
		signer:          types.NewLondonSigner(chainConfig.ChainID),
		makeEthash:      makeEthash,
	}
}

func (p *Parlia) IsSystemTransaction(tx *types.Transaction, header *types.Header) (bool, error) {
	// deploy a contract
	if tx.To() == nil || p.chainConfig.PrimordialPulseAhead(header.Number) {
		return false, nil
	}
	sender, err := types.Sender(p.signer, tx)
	if err != nil {
		log.Debug(fmt.Sprintf("IsSystemTransaction Error:%v\n", err))
		log.Trace(fmt.Sprintf("IsSystemTransaction Error:\nError: %v\nSigner: \n%s\nTX: \n%s\nHeader: \n%s\n", err, spew.Sdump(p.signer), spew.Sdump(tx), spew.Sdump(header)))
		return false, errors.New("UnAuthorized transaction")
	}
	if sender == header.Coinbase && isToSystemContract(*tx.To()) && tx.GasPrice().Cmp(big.NewInt(0)) == 0 {
		return true, nil
	}
	return false, nil
}

func (p *Parlia) IsSystemContract(to *common.Address) bool {
	if to == nil {
		return false
	}
	return isToSystemContract(*to)
}

// Author implements consensus.Engine, returning the SystemAddress
func (p *Parlia) Author(header *types.Header) (common.Address, error) {
	if p.chainConfig.PrimordialPulseAhead(header.Number) {
		return p.ethash().Author(header)
	}
	return header.Coinbase, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules.
func (p *Parlia) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	if p.chainConfig.PrimordialPulseAhead(header.Number) {
		return p.ethash().VerifyHeader(chain, header, seal)
	}
	return p.verifyHeader(chain, header, nil)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers. The
// method returns a quit channel to abort the operations and a results channel to
// retrieve the async verifications (the order is that of the input slice).
func (p *Parlia) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		forkAhead := p.chainConfig.PrimordialPulseAhead(headers[0].Number)
		if forkAhead {
			forkIdx := p.chainConfig.PrimordialPulseBlock.Uint64() - headers[0].Number.Uint64()
			if forkIdx > uint64(len(headers)) {
				forkIdx = uint64(len(headers))
			}

			preforkHeaders := headers[:forkIdx]
			_, res := p.ethash().VerifyHeaders(chain, preforkHeaders, seals)
			for i := 0; i < len(preforkHeaders); i++ {
				err := <-res
				select {
				case <-abort:
					return
				case results <- err:
				}
			}

			if forkIdx == uint64(len(headers)) {
				// all blocks are pre-fork, we can stop now
				return
			}

			// we need to continue on the post-fork blocks with this consensus engine
			// include the last prefork header for parent verification of first postfork block
			headers = headers[forkIdx-1:]
		}

		for i, header := range headers {
			if i == 0 && forkAhead {
				// the list of headers includes prefork and postfork blocks
				// the first block in this slice is the final (already verified) prefork block
				// used only for parent verification of first postfork block
				continue
			}
			err := p.verifyHeader(chain, header, headers[:i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (p *Parlia) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	if header.Number == nil {
		return errUnknownBlock
	}
	number := header.Number.Uint64()

	// Don't waste time checking blocks from the future
	if header.Time > uint64(time.Now().Unix()) {
		return consensus.ErrFutureBlock
	}
	// Check that the extra-data contains the vanity, validators and signature.
	if len(header.Extra) < extraVanity {
		return errMissingVanity
	}
	if len(header.Extra) < extraVanity+extraSeal {
		return errMissingSignature
	}
	// check extra data
	isEpoch := number%p.config.Epoch == 0

	// Ensure that the extra-data contains a signer list on checkpoint, but none otherwise
	signersBytes := len(header.Extra) - extraVanity - extraSeal
	if !isEpoch && signersBytes != 0 {
		return errExtraValidators
	}

	if isEpoch && signersBytes%validatorBytesLength != 0 {
		return errInvalidSpanValidators
	}

	// Ensure that the mix digest is zero as we don't have fork protection currently
	if header.MixDigest != (common.Hash{}) {
		return errInvalidMixDigest
	}
	// Ensure that the block doesn't contain any uncles which are meaningless in PoA
	if header.UncleHash != uncleHash {
		return errInvalidUncleHash
	}
	// Ensure that the block's difficulty is meaningful (may not be correct at this point)
	if number > 0 {
		if header.Difficulty == nil {
			return errInvalidDifficulty
		}
	}
	// If all checks passed, validate any special fields for hard forks
	if err := misc.VerifyForkHashes(chain.Config(), header, false); err != nil {
		return err
	}
	// All basic checks passed, verify cascading fields
	return p.verifyCascadingFields(chain, header, parents)
}

// verifyCascadingFields verifies all the header fields that are not standalone,
// rather depend on a batch of previous headers. The caller may optionally pass
// in a batch of parents (ascending order) to avoid looking those up from the
// database. This is useful for concurrently verifying a batch of new headers.
func (p *Parlia) verifyCascadingFields(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// The genesis block is the always valid dead-end
	number := header.Number.Uint64()
	if number == 0 {
		return nil
	}

	var parent *types.Header
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}

	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return consensus.ErrUnknownAncestor
	}

	snap, err := p.snapshot(chain, number-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	err = p.blockTimeVerifyForRamanujanFork(snap, header, parent)
	if err != nil {
		return err
	}

	// Verify that the gas limit is <= 2^63-1
	capacity := uint64(0x7fffffffffffffff)
	if header.GasLimit > capacity {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, capacity)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	// Verify that the gas limit remains within allowed bounds
	diff := int64(parent.GasLimit) - int64(header.GasLimit)
	if diff < 0 {
		diff *= -1
	}
	limit := parent.GasLimit / params.GasLimitBoundDivisor

	if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
		return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
	}

	// All basic checks passed, verify the seal and return
	return p.verifySeal(chain, header, parents)
}

func (p *Parlia) ethash() *ethash.Ethash {
	if p._ethash != nil {
		log.Debug("Deferring to ethash")
		return p._ethash
	}
	log.Debug("Instantiating ethash")
	p._ethash = p.makeEthash()
	return p._ethash
}

// snapshot retrieves the authorization snapshot at a given point in time.
func (p *Parlia) snapshot(chain consensus.ChainHeaderReader, number uint64, hash common.Hash, parents []*types.Header) (*Snapshot, error) {
	// Search for a snapshot in memory or on disk for checkpoints
	var (
		headers []*types.Header
		snap    *Snapshot
	)

	for snap == nil {
		// If an in-memory snapshot was found, use that
		if s, ok := p.recentSnaps.Get(hash); ok {
			snap = s.(*Snapshot)
			break
		}

		// If an on-disk checkpoint snapshot can be found, use that
		if number%checkpointInterval == 0 {
			if s, err := loadSnapshot(p.config, p.signatures, p.db, hash, p.ethAPI); err == nil {
				log.Trace("Loaded snapshot from disk", "number", number, "hash", hash)
				snap = s
				break
			}
		}

		// If we're at the genesis, snapshot the initial state.
		if number == 0 {
			checkpoint := chain.GetHeaderByNumber(number)
			if checkpoint != nil {
				// get checkpoint data
				hash := checkpoint.Hash()

				if p.chainConfig.PrimordialPulseAhead(common.Big0) {
					// If we're at the genesis, but there is a PrimordialPulse fork in our future,
					// this implies that we're behind the forked chain. Suppose an empty set of validators
					// and wait for chain synchronization.
					snap = newSnapshot(p.config, p.signatures, number, hash, []common.Address{{}}, p.ethAPI)
				} else {
					// Initialize the validators from the genesis block extra-data.
					validatorBytes := checkpoint.Extra[extraVanity : len(checkpoint.Extra)-extraSeal]
					validators, err := ParseValidators(validatorBytes)
					if err != nil {
						return nil, err
					}

					snap = newSnapshot(p.config, p.signatures, number, hash, validators, p.ethAPI)
				}

				if err := snap.store(p.db); err != nil {
					return nil, err
				}
				log.Info("Stored checkpoint snapshot to disk", "number", number, "hash", hash)
				break
			}
		}

		// If we're at the PrimordialPulseBlock, snapshot the initial state from the validator contract.
		// This will only occur for a non-zero primordial block, which indicates a fork of an existing chain.
		// In such a case we initialize the validator snapshot from ParliaConfig instead of genesis block headers.
		// Offset by one since this is function is looking for the snapshot based on previous block.
		if p.chainConfig.IsPrimordialPulseBlock(number + 1) {
			validators, err := p.initPulsors()
			if err != nil {
				return nil, err
			}

			snap = newSnapshot(p.config, p.signatures, number, hash, validators, p.ethAPI)

			// Store the snapshot to cache instead of disk since the block may or may not fall on the checkpointInterval.
			// The snap will load from cache, or worst case be reconstructed from config again.
			p.recentSnaps.Add(snap.Hash, snap)
			log.Info("Added primordial pulse snapshot to cache", "number", number, "hash", hash)
			break
		}

		// No snapshot for this header, gather the header and move backward
		var header *types.Header
		if len(parents) > 0 {
			// If we have explicit parents, pick from there (enforced)
			header = parents[len(parents)-1]
			if header.Hash() != hash || header.Number.Uint64() != number {
				return nil, consensus.ErrUnknownAncestor
			}
			parents = parents[:len(parents)-1]
		} else {
			// No explicit parents (or no more left), reach out to the database
			header = chain.GetHeader(hash, number)
			if header == nil {
				return nil, consensus.ErrUnknownAncestor
			}
		}
		headers = append(headers, header) // headers appended in descending order
		number, hash = number-1, header.ParentHash
	}

	// check if snapshot is nil
	if snap == nil {
		return nil, fmt.Errorf("unknown error while retrieving snapshot at block number %v", number)
	}

	// Previous snapshot found, apply any pending headers on top of it
	for i := 0; i < len(headers)/2; i++ {
		// reverse the order of the headers array (sort ascending)
		headers[i], headers[len(headers)-1-i] = headers[len(headers)-1-i], headers[i]
	}

	snap, err := snap.apply(headers, chain, parents, p.chainConfig.ChainID)
	if err != nil {
		return nil, err
	}
	p.recentSnaps.Add(snap.Hash, snap)

	// If we've generated a new checkpoint snapshot, save to disk
	if snap.Number%checkpointInterval == 0 && len(headers) > 0 {
		if err = snap.store(p.db); err != nil {
			return nil, err
		}
		log.Trace("Stored snapshot to disk", "number", snap.Number, "hash", snap.Hash)
	}
	return snap, err
}

// VerifyUncles implements consensus.Engine, always returning an error for any
// uncles as this consensus mechanism doesn't permit uncles.
func (p *Parlia) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if p.chainConfig.PrimordialPulseAhead(block.Header().Number) {
		return p.ethash().VerifyUncles(chain, block)
	}
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

// VerifySeal implements consensus.Engine, checking whether the signature contained
// in the header satisfies the consensus protocol requirements.
func (p *Parlia) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return p.verifySeal(chain, header, nil)
}

// verifySeal checks whether the signature contained in the header satisfies the
// consensus protocol requirements. The method accepts an optional list of parent
// headers that aren't yet part of the local blockchain to generate the snapshots
// from.
func (p *Parlia) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// Verifying the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	// Retrieve the snapshot needed to verify this header and cache it
	snap, err := p.snapshot(chain, number-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	// Resolve the authorization key and check against validators
	signer, err := ecrecover(header, p.signatures, p.chainConfig.ChainID)
	if err != nil {
		return err
	}

	if signer != header.Coinbase {
		return errCoinBaseMisMatch
	}

	if _, ok := snap.Validators[signer]; !ok {
		return &consensus.ErrUnauthorizedValidator{}
	}

	for seen, recent := range snap.Recents {
		if recent == signer {
			// Signer is among recents, only fail if the current block doesn't shift it out
			if limit := uint64(len(snap.Validators)/2 + 1); seen > number-limit {
				return errRecentlySigned
			}
		}
	}

	// Ensure that the difficulty corresponds to the turn-ness of the signer
	if !p.fakeDiff {
		inturn := snap.inturn(signer)
		if inturn && header.Difficulty.Cmp(diffInTurn) != 0 {
			return errWrongDifficulty
		}
		if !inturn && header.Difficulty.Cmp(diffNoTurn) != 0 {
			return errWrongDifficulty
		}
	}

	return nil
}

// Prepare implements consensus.Engine, preparing all the consensus fields of the
// header for running the transactions on top.
func (p *Parlia) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	// Bail out early if the Authorize() method for block mining has not been called
	if p.signTxFn == nil {
		return &consensus.ErrUnauthorizedValidator{}
	}

	header.Coinbase = p.val
	header.Nonce = types.BlockNonce{}

	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	// Set the correct difficulty
	header.Difficulty = calcDifficulty(snap, p.val)

	// Ensure the extra data has all it's components
	if len(header.Extra) < extraVanity-nextForkHashSize {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, extraVanity-nextForkHashSize-len(header.Extra))...)
	}
	header.Extra = header.Extra[:extraVanity-nextForkHashSize]
	nextForkHash := forkid.NextForkHash(p.chainConfig, p.genesisHash, number)
	header.Extra = append(header.Extra, nextForkHash[:]...)

	if number%p.config.Epoch == 0 {
		newValidatorBytes, err := p.getEpochValidatorBytes(header, snap)
		if err != nil {
			return err
		}

		header.Extra = append(header.Extra, newValidatorBytes...)
	}

	// add extra seal space
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)

	// Mix digest is reserved for now, set to empty
	header.MixDigest = common.Hash{}

	// Ensure the timestamp has the correct delay
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Time = p.blockTimeForRamanujanFork(snap, header, parent)
	if header.Time < uint64(time.Now().Unix()) {
		header.Time = uint64(time.Now().Unix())
	}
	return nil
}

// Finalize implements consensus.Engine, ensuring no uncles are set, nor block
// rewards given.
func (p *Parlia) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs *[]*types.Transaction,
	uncles []*types.Header, receipts *[]*types.Receipt, systemTxs *[]*types.Transaction, usedGas *uint64) error {
	if p.chainConfig.PrimordialPulseAhead(header.Number) {
		return p.ethash().Finalize(chain, header, state, txs, uncles, receipts, systemTxs, usedGas)
	}
	// warn if not in majority fork
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		panic(err)
	}
	nextForkHash := forkid.NextForkHash(p.chainConfig, p.genesisHash, number)
	if !snap.isMajorityFork(hex.EncodeToString(nextForkHash[:])) {
		log.Debug("there is a possible fork, and your client is not the majority. Please check...", "nextForkHash", hex.EncodeToString(nextForkHash[:]))
	}

	// If the block is an epoch start block, verify the validator list
	// The verification can only be done when the state is ready, it can't be done in VerifyHeader.
	if number%p.config.Epoch == 0 {
		validatorsBytes, err := p.getEpochValidatorBytes(header, snap)
		if err != nil {
			return err
		}

		extraSuffix := len(header.Extra) - extraSeal
		if !bytes.Equal(header.Extra[extraVanity:extraSuffix], validatorsBytes) {
			return errMismatchingEpochValidators
		}
	}

	// No block rewards in PoA, so the state remains as is and uncles are dropped
	cx := chainContext{Chain: chain, parlia: p}
	if header.Number.Cmp(common.Big1) == 0 || p.chainConfig.IsPrimordialPulseBlock(number) {
		log.Info("Initializing system contracts", "number", number)
		if err := p.initContracts(state, header, cx, txs, receipts, systemTxs, usedGas, false); err != nil {
			log.Error("Failed to initialize system contracts")
		}

		if p.chainConfig.IsPrimordialPulseBlock(number) {
			// handle initial allocations for the primordialPulse fork
			if err := p.primordialPulseAlloctions(state); err != nil {
				panic(err)
			}
		}
	}

	// trigger the validator rotation on the last block of the era
	// on the next block (epoch), the authorization snapshot will be updated
	if (number+1)%(p.config.Epoch*p.config.Era) == 0 {
		log.Info("Triggering staked validator rotation", "number", number)
		err = p.rotateValidators(state, header, cx, txs, receipts, systemTxs, usedGas, false)
		if err != nil {
			log.Error("Staked validator rotation failed", "number", number)
		}
	}

	if header.Difficulty.Cmp(diffInTurn) != 0 {
		spoiledVal := snap.supposeValidator()
		signedRecently := false
		for _, recent := range snap.Recents {
			if recent == spoiledVal {
				signedRecently = true
				break
			}
		}
		if !signedRecently {
			log.Trace("slash validator", "block hash", header.Hash(), "address", spoiledVal)
			err = p.slash(spoiledVal, state, header, cx, txs, receipts, systemTxs, usedGas, false)
			if err != nil {
				// it is possible that slash validator failed because of the slash channel is disabled.
				log.Error("slash validator failed", "block hash", header.Hash(), "address", spoiledVal)
			}
		}
	}

	val := header.Coinbase
	if err := p.distributeIncoming(val, state, header, cx, txs, receipts, systemTxs, usedGas, false); err != nil {
		panic(err)
	}
	if len(*systemTxs) > 0 {
		return errors.New("the length of systemTxs do not match")
	}
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	header.UncleHash = types.CalcUncleHash(nil)
	return nil
}

// FinalizeAndAssemble implements consensus.Engine, ensuring no uncles are set,
// nor block rewards given, and returns the final block.
func (p *Parlia) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB,
	txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, []*types.Receipt, error) {
	// No block rewards in PoA, so the state remains as is and uncles are dropped
	cx := chainContext{Chain: chain, parlia: p}
	if txs == nil {
		txs = make([]*types.Transaction, 0)
	}
	if receipts == nil {
		receipts = make([]*types.Receipt, 0)
	}

	number := header.Number.Uint64()
	if header.Number.Cmp(common.Big1) == 0 || p.chainConfig.IsPrimordialPulseBlock(number) {
		log.Info("Initializing system contracts", "number", header.Number)

		if err := p.initContracts(state, header, cx, &txs, &receipts, nil, &header.GasUsed, true); err != nil {
			log.Error("Failed to initialize system contracts")
		}

		if p.chainConfig.IsPrimordialPulseBlock(number) {
			// handle initial allocations for the primordialPulse fork
			if err := p.primordialPulseAlloctions(state); err != nil {
				panic(err)
			}
		}
	}

	// trigger the validator rotation on the last block of the era
	// on the next block (epoch), the authorization snapshot will be updated
	if (number+1)%(p.config.Epoch*p.config.Era) == 0 {
		log.Info("Triggering staked validator rotation", "number", number)
		err := p.rotateValidators(state, header, cx, &txs, &receipts, nil, &header.GasUsed, true)
		if err != nil {
			log.Error("Staked validator rotation failed", "number", number)
		}
	}

	if header.Difficulty.Cmp(diffInTurn) != 0 {
		snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
		if err != nil {
			panic(err)
		}
		spoiledVal := snap.supposeValidator()
		signedRecently := false
		for _, recent := range snap.Recents {
			if recent == spoiledVal {
				signedRecently = true
				break
			}
		}
		if !signedRecently {
			err = p.slash(spoiledVal, state, header, cx, &txs, &receipts, nil, &header.GasUsed, true)
			if err != nil {
				// it is possible that slash validator failed because of the slash channel is disabled.
				log.Error("slash validator failed", "block hash", header.Hash(), "address", spoiledVal)
			}
		}
	}

	err := p.distributeIncoming(p.val, state, header, cx, &txs, &receipts, nil, &header.GasUsed, true)
	if err != nil {
		panic(err)
	}
	// should not happen. Once happen, stop the node is better than broadcast the block
	if header.GasLimit < header.GasUsed {
		panic("Gas consumption of system txs exceed the gas limit")
	}
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	header.UncleHash = types.CalcUncleHash(nil)

	// Assemble and return the final block for sealing
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil)), receipts, nil
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (p *Parlia) Authorize(val common.Address, signFn SignerFn, signTxFn SignerTxFn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.val = val
	p.signFn = signFn
	p.signTxFn = signTxFn
}

func (p *Parlia) Delay(chain consensus.ChainReader, header *types.Header) *time.Duration {
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return nil
	}
	delay := p.delayForRamanujanFork(snap, header)
	return &delay
}

// Seal implements consensus.Engine, attempting to create a sealed block using
// the local signing credentials.
func (p *Parlia) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	header := block.Header()

	// Sealing the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	// For 0-period chains, refuse to seal empty blocks (no reward but would spin sealing)
	if p.config.Period == 0 && len(block.Transactions()) == 0 {
		log.Info("Sealing paused, waiting for transactions")
		return nil
	}
	// Don't hold the val fields for the entire sealing procedure
	p.lock.RLock()
	val, signFn := p.val, p.signFn
	p.lock.RUnlock()

	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	// Bail out if we're unauthorized to sign a block
	if _, authorized := snap.Validators[val]; !authorized {
		return &consensus.ErrUnauthorizedValidator{}
	}

	// If we're amongst the recent signers, wait for the next block
	for seen, recent := range snap.Recents {
		if recent == val {
			// Signer is among recents, only wait if the current block doesn't shift it out
			if limit := uint64(len(snap.Validators)/2 + 1); number < limit || seen > number-limit {
				log.Info("Signed recently, must wait for others")
				return nil
			}
		}
	}

	// Sweet, the protocol permits us to sign the block, wait for our time
	delay := p.delayForRamanujanFork(snap, header)

	log.Info("Sealing block with", "number", number, "delay", delay, "headerDifficulty", header.Difficulty, "val", val.Hex())

	// Sign all the things!
	sig, err := signFn(accounts.Account{Address: val}, accounts.MimetypeParlia, ParliaRLP(header, p.chainConfig.ChainID))
	if err != nil {
		return err
	}
	copy(header.Extra[len(header.Extra)-extraSeal:], sig)

	// Wait until sealing is terminated or delay timeout.
	log.Trace("Waiting for slot to sign and propagate", "delay", common.PrettyDuration(delay))
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}

		select {
		case results <- block.WithSeal(header):
		default:
			log.Warn("Sealing result is not read by miner", "sealhash", SealHash(header, p.chainConfig.ChainID))
		}
	}()

	return nil
}

func (p *Parlia) EnoughDistance(chain consensus.ChainReader, header *types.Header) bool {
	snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
	if err != nil {
		return true
	}
	return snap.enoughDistance(p.val, header)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func (p *Parlia) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	if p.chainConfig.PrimordialPulseAhead(new(big.Int).Add(parent.Number, common.Big1)) {
		return p.ethash().CalcDifficulty(chain, time, parent)
	}
	snap, err := p.snapshot(chain, parent.Number.Uint64(), parent.Hash(), nil)
	if err != nil {
		return nil
	}
	return calcDifficulty(snap, p.val)
}

// calcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func calcDifficulty(snap *Snapshot, signer common.Address) *big.Int {
	if snap.inturn(signer) {
		return new(big.Int).Set(diffInTurn)
	}
	return new(big.Int).Set(diffNoTurn)
}

// SealHash returns the hash of a block prior to it being sealed.
func (p *Parlia) SealHash(header *types.Header) common.Hash {
	return SealHash(header, p.chainConfig.ChainID)
}

// APIs implements consensus.Engine, returning the user facing RPC API to query snapshot.
func (p *Parlia) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	return []rpc.API{{
		Namespace: "parlia",
		Version:   "1.0",
		Service:   &API{chain: chain, parlia: p},
		Public:    false,
	}}
}

// Close implements consensus.Engine. It's a noop for parlia as there are no background threads.
func (p *Parlia) Close() error {
	return nil
}

// ==========================  interaction with contract/account =========

// getCurrentValidators get current validators
func (p *Parlia) getCurrentValidators(blockHash common.Hash) ([]common.Address, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	// method
	method := "getValidators"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := p.validatorSetABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for getValidators", "error", err)
		return nil, err
	}
	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.ValidatorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, blockNr, nil)
	if err != nil {
		return nil, err
	}

	var (
		ret0 = new([]common.Address)
	)
	out := ret0

	if err := p.validatorSetABI.UnpackIntoInterface(out, method, result); err != nil {
		return nil, err
	}

	valz := make([]common.Address, len(*ret0))
	for i, a := range *ret0 {
		valz[i] = a
	}
	return valz, nil
}

// distribute incoming accrued fees
func (p *Parlia) distributeIncoming(val common.Address, state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	coinbase := header.Coinbase
	balance := state.GetBalance(consensus.SystemAddress)
	if balance.Cmp(common.Big0) <= 0 {
		return nil
	}
	state.SetBalance(consensus.SystemAddress, big.NewInt(0))

	burn := big.NewInt(0)
	if p.config.BurnRate > 0 {
		burn = burn.Div(balance, big.NewInt(int64(p.config.BurnRate)))
		state.AddBalance(common.HexToAddress(systemcontracts.FeeBurnContract), burn)
		log.Debug("🔥 transaction fee burn", "number", header.Number, "amount", burn)
	}

	reward := new(big.Int).Sub(balance, burn)
	state.AddBalance(coinbase, reward)
	log.Trace("distribute to validator contract", "block hash", header.Hash(), "amount", reward)
	return p.distributeToValidator(reward, val, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
}

// rotateValidators triggers the staked validator rotation
func (p *Parlia) rotateValidators(state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	// method
	method := "rotateValidators"

	// get packed data
	data, err := p.stakingABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for staking contract", "error", err)
		return err
	}
	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.StakingContract), data, common.Big0)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
}

// slash spoiled validators
func (p *Parlia) slash(spoiledVal common.Address, state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	// method
	method := "slash"

	// get packed data
	data, err := p.slashABI.Pack(method,
		spoiledVal,
	)
	if err != nil {
		log.Error("Unable to pack tx for slash contract", "error", err)
		return err
	}
	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.SlashingContract), data, common.Big0)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
}

// init contracts
func (p *Parlia) initContracts(state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	// method
	method := "init"
	// contracts
	contracts := []string{
		// slash contract does not require initialization
		systemcontracts.ValidatorContract,
		systemcontracts.StakingContract,
	}
	// get packed data
	data, err := p.validatorSetABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for init validator set", "error", err)
		return err
	}
	for _, c := range contracts {
		msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(c), data, common.Big0)
		// apply message
		log.Trace("init contract", "block hash", header.Hash(), "contract", c)
		err = p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
		if err != nil {
			return err
		}
	}
	return nil
}

// award funds to the validator
func (p *Parlia) distributeToValidator(amount *big.Int, validator common.Address,
	state *state.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool) error {
	// method
	method := "deposit"

	// get packed data
	data, err := p.validatorSetABI.Pack(method,
		validator,
	)
	if err != nil {
		log.Error("Unable to pack tx for deposit", "error", err)
		return err
	}
	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.ValidatorContract), data, amount)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining)
}

// get system message
func (p *Parlia) getSystemMessage(from, toAddress common.Address, data []byte, value *big.Int) callmsg {
	return callmsg{
		ethereum.CallMsg{
			From:     from,
			Gas:      math.MaxUint64 / 2,
			GasPrice: big.NewInt(0),
			Value:    value,
			To:       &toAddress,
			Data:     data,
		},
	}
}

func (p *Parlia) applyTransaction(
	msg callmsg,
	state *state.StateDB,
	header *types.Header,
	chainContext core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt,
	receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool,
) (err error) {
	nonce := state.GetNonce(msg.From())
	expectedTx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       msg.To(),
		Value:    msg.Value(),
		Gas:      msg.Gas(),
		GasPrice: msg.GasPrice(),
		Data:     msg.Data(),
	})
	expectedHash := p.signer.Hash(expectedTx)

	if msg.From() == p.val && mining {
		expectedTx, err = p.signTxFn(accounts.Account{Address: msg.From()}, expectedTx, p.chainConfig.ChainID)
		if err != nil {
			return err
		}
	} else {
		if receivedTxs == nil || len(*receivedTxs) == 0 || (*receivedTxs)[0] == nil {
			return errors.New("supposed to get a actual transaction, but get none")
		}
		actualTx := (*receivedTxs)[0]
		if !bytes.Equal(p.signer.Hash(actualTx).Bytes(), expectedHash.Bytes()) {
			return fmt.Errorf("expected tx hash %v, get %v", expectedHash.String(), actualTx.Hash().String())
		}
		expectedTx = actualTx
		// move to next
		*receivedTxs = (*receivedTxs)[1:]
	}
	state.Prepare(expectedTx.Hash(), len(*txs))
	gasUsed, err := applyMessage(msg, state, header, p.chainConfig, chainContext)
	if err != nil {
		return err
	}
	*txs = append(*txs, expectedTx)
	var root []byte
	if p.chainConfig.IsByzantium(header.Number) {
		state.Finalise(true)
	} else {
		root = state.IntermediateRoot(p.chainConfig.IsEIP158(header.Number)).Bytes()
	}
	*usedGas += gasUsed
	receipt := &types.Receipt{
		Type:              types.LegacyTxType,
		PostState:         common.CopyBytes(root),
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: *usedGas,
	}
	receipt.TxHash = expectedTx.Hash()
	receipt.GasUsed = gasUsed

	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = state.GetLogs(expectedTx.Hash(), header.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = header.Hash()
	receipt.BlockNumber = header.Number
	receipt.TransactionIndex = uint(state.TxIndex())
	*receipts = append(*receipts, receipt)
	state.SetNonce(msg.From(), nonce+1)
	return nil
}

// ===========================     utility function        ==========================
// SealHash returns the hash of a block prior to it being sealed.
func SealHash(header *types.Header, chainId *big.Int) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()
	encodeSigHeader(hasher, header, chainId)
	hasher.Sum(hash[:0])
	return hash
}

func encodeSigHeader(w io.Writer, header *types.Header, chainId *big.Int) {
	err := rlp.Encode(w, []interface{}{
		chainId,
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:len(header.Extra)-65], // this will panic if extra is too short, should check before calling encodeSigHeader
		header.MixDigest,
		header.Nonce,
	})
	if err != nil {
		panic("can't encode: " + err.Error())
	}
}

func backOffTime(snap *Snapshot, val common.Address) uint64 {
	if snap.inturn(val) {
		return 0
	} else {
		idx := snap.indexOfVal(val)
		if idx < 0 {
			// The backOffTime does not matter when a validator is not authorized.
			return 0
		}
		s := rand.NewSource(int64(snap.Number))
		r := rand.New(s)
		n := len(snap.Validators)
		backOffSteps := make([]uint64, 0, n)
		for idx := uint64(0); idx < uint64(n); idx++ {
			backOffSteps = append(backOffSteps, idx)
		}
		r.Shuffle(n, func(i, j int) {
			backOffSteps[i], backOffSteps[j] = backOffSteps[j], backOffSteps[i]
		})
		delay := initialBackOffTime + backOffSteps[idx]*wiggleTime
		return delay
	}
}

// chain context
type chainContext struct {
	Chain  consensus.ChainHeaderReader
	parlia consensus.Engine
}

func (c chainContext) Engine() consensus.Engine {
	return c.parlia
}

func (c chainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return c.Chain.GetHeader(hash, number)
}

// callmsg implements core.Message to allow passing it as a transaction simulator.
type callmsg struct {
	ethereum.CallMsg
}

func (m callmsg) From() common.Address { return m.CallMsg.From }
func (m callmsg) Nonce() uint64        { return 0 }
func (m callmsg) CheckNonce() bool     { return false }
func (m callmsg) To() *common.Address  { return m.CallMsg.To }
func (m callmsg) GasPrice() *big.Int   { return m.CallMsg.GasPrice }
func (m callmsg) Gas() uint64          { return m.CallMsg.Gas }
func (m callmsg) Value() *big.Int      { return m.CallMsg.Value }
func (m callmsg) Data() []byte         { return m.CallMsg.Data }

// apply message
func applyMessage(
	msg callmsg,
	state *state.StateDB,
	header *types.Header,
	chainConfig *params.ChainConfig,
	chainContext core.ChainContext,
) (uint64, error) {
	// Create a new context to be used in the EVM environment
	context := core.NewEVMBlockContext(header, chainContext, nil)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(context, vm.TxContext{Origin: msg.From(), GasPrice: big.NewInt(0)}, state, chainConfig, vm.Config{})
	// Apply the transaction to the current state (included in the env)
	ret, returnGas, err := vmenv.Call(
		vm.AccountRef(msg.From()),
		*msg.To(),
		msg.Data(),
		msg.Gas(),
		msg.Value(),
	)
	if err != nil {
		log.Error("apply message failed", "msg", string(ret), "err", err)
	}
	return msg.Gas() - returnGas, err
}
