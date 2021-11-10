package parlia

import (
	"time"

	"gitlab.com/pulsechaincom/go-pulse/consensus"
	"gitlab.com/pulsechaincom/go-pulse/core/types"
)

const (
	wiggleTimeBeforeFork       = 500 * time.Millisecond // Random delay (per signer) to allow concurrent signers
	fixedBackOffTimeBeforeFork = 200 * time.Millisecond
)

func (p *Parlia) delayForRamanujanFork(snap *Snapshot, header *types.Header) time.Duration {
	return time.Until(time.Unix(int64(header.Time), 0))
}

func (p *Parlia) blockTimeForRamanujanFork(snap *Snapshot, header, parent *types.Header) uint64 {
	return parent.Time + p.config.Period + backOffTime(snap, p.val)
}

func (p *Parlia) blockTimeVerifyForRamanujanFork(snap *Snapshot, header, parent *types.Header) error {
	if header.Time < parent.Time+p.config.Period+backOffTime(snap, header.Coinbase) {
		return consensus.ErrFutureBlock
	}

	return nil
}
