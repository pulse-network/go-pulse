// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package downloader

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// syncState starts downloading state with the given root hash.
func (d *Downloader) syncState(root common.Hash) *stateSync {
	// Create the state sync
	s := newStateSync(d, root)
	select {
	case d.stateSyncStart <- s:
		// If we tell the statesync to restart with a new root, we also need
		// to wait for it to actually also start -- when old requests have timed
		// out or been delivered
		<-s.started
	case <-d.quitCh:
		s.err = errCancelStateFetch
		close(s.done)
	}
	return s
}

// stateFetcher manages the active state sync and accepts requests
// on its behalf.
func (d *Downloader) stateFetcher() {
	for {
		select {
		case s := <-d.stateSyncStart:
			for next := s; next != nil; {
				next = d.runStateSync(next)
			}
		case <-d.quitCh:
			return
		}
	}
}

// runStateSync runs a state synchronisation until it completes or another root
// hash is requested to be switched over to.
func (d *Downloader) runStateSync(s *stateSync) *stateSync {
	log.Trace("State sync starting", "root", s.root)

	go s.run()
	defer s.Cancel()

	for {
		select {
		case next := <-d.stateSyncStart:
			d.spindownStateSync(active, finished, timeout, peerDrop)
			return next

		case <-s.done:
			d.spindownStateSync(active, finished, timeout, peerDrop)
			return nil
		}
	}
}

// spindownStateSync 'drains' the outstanding requests; some will be delivered and other
// will time out. This is to ensure that when the next stateSync starts working, all peers
// are marked as idle and de facto _are_ idle.
func (d *Downloader) spindownStateSync(active map[string]*stateReq, finished []*stateReq, timeout chan *stateReq, peerDrop chan *peerConnection) {
	log.Trace("State sync spinning down", "active", len(active), "finished", len(finished))
	for len(active) > 0 {
		var (
			req    *stateReq
			reason string
		)
		select {
		// Handle (drop) incoming state packs:
		case pack := <-d.stateCh:
			req = active[pack.PeerId()]
			reason = "delivered"
		// Handle dropped peer connections:
		case p := <-peerDrop:
			req = active[p.id]
			reason = "peerdrop"
		// Handle timed-out requests:
		case req = <-timeout:
			reason = "timeout"
		}
		if req == nil {
			continue
		}
		req.peer.log.Trace("State peer marked idle (spindown)", "req.items", int(req.nItems), "reason", reason)
		req.timer.Stop()
		delete(active, req.peer.id)
		req.peer.SetNodeDataIdle(int(req.nItems), time.Now())
	}
	// The 'finished' set contains deliveries that we were going to pass to processing.
	// Those are now moot, but we still need to set those peers as idle, which would
	// otherwise have been done after processing
	for _, req := range finished {
		req.peer.SetNodeDataIdle(int(req.nItems), time.Now())
	}
}

// stateSync schedules requests for downloading a particular state trie defined
// by a given state root.
type stateSync struct {
	d    *Downloader // Downloader instance to access and manage current peerset
	root common.Hash // State root currently being synced

	started    chan struct{} // Started is signalled once the sync loop starts
	cancel     chan struct{} // Channel to signal a termination request
	cancelOnce sync.Once     // Ensures cancel only ever gets called once
	done       chan struct{} // Channel to signal termination completion
	err        error         // Any error hit during sync (set before completion)
}

// newStateSync creates a new state trie download scheduler. This method does not
// yet start the sync. The user needs to call run to initiate.
func newStateSync(d *Downloader, root common.Hash) *stateSync {
	return &stateSync{
		d:       d,
		root:    root,
		cancel:  make(chan struct{}),
		done:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

// run starts the task assignment and response processing loop, blocking until
// it finishes, and finally notifying any goroutines waiting for the loop to
// finish.
func (s *stateSync) run() {
	close(s.started)
	s.err = s.d.SnapSyncer.Sync(s.root, s.cancel)
	close(s.done)
}

// Wait blocks until the sync is done or canceled.
func (s *stateSync) Wait() error {
	<-s.done
	return s.err
}

// Cancel cancels the sync and waits until it has shut down.
func (s *stateSync) Cancel() error {
	s.cancelOnce.Do(func() {
		close(s.cancel)
	})
	return s.Wait()
}
