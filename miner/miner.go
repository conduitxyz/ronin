// Copyright 2014 The go-ethereum Authors
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

// Package miner implements Ethereum block creation and mining.
package miner

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// Backend wraps all core module methods required for mining operations.
type Backend interface {
	BlockChain() *core.BlockChain
	TxPool() *txpool.TxPool
}

// Config holds the configuration parameters for the Miner.
type Config struct {
	Etherbase            common.Address `toml:",omitempty"` // Public address for block mining rewards (default = first account)
	Notify               []string       `toml:",omitempty"` // HTTP URL list to be notified of new work packages (only useful in ethash).
	NotifyFull           bool           `toml:",omitempty"` // Notify with pending block headers instead of work packages
	ExtraData            hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	GasFloor             uint64         // Target gas floor for mined blocks.
	GasCeil              uint64         // Target gas ceiling for mined blocks.
	GasPrice             *big.Int       // Minimum gas price for mining a transaction (used for transaction selection logic)
	Recommit             time.Duration  // The time interval for the miner to re-create mining work (to update transactions/state).
	Noverify             bool           // Disable remote mining solution verification (only useful in ethash).
	BlockProduceLeftOver time.Duration  // Time reserved for block assembly after sealing begins.
	BlockSizeReserve     uint64         // Reserve size for block data/receipts.
}

// Miner acts as the orchestrator for block creation and proof-of-work searching.
// It manages the worker lifecycle and handles external synchronization events.
type Miner struct {
	mux      *event.TypeMux
	worker   *worker
	coinbase common.Address // Current recipient for block rewards (Etherbase)
	eth      Backend
	engine   consensus.Engine
	exitCh   chan struct{}  // Channel to signal the update loop to terminate
	startCh  chan common.Address // Channel to signal the miner to start with a new coinbase
	stopCh   chan struct{}  // Channel to signal the miner to stop
	wg       sync.WaitGroup // WaitGroup to ensure the update loop exits cleanly
}

// New creates a new Miner instance, initializing its internal worker and starting the update loop.
func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(block *types.Block) bool) *Miner {
	miner := &Miner{
		eth:    eth,
		mux:    mux,
		engine: engine,
		exitCh: make(chan struct{}),
		startCh: make(chan common.Address),
		stopCh: make(chan struct{}),
		// The worker is the core component that handles the actual block assembly and sealing.
		worker: newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, true),
	}
	miner.wg.Add(1)
	go miner.update()
	return miner
}

// update keeps track of the downloader events and manages the worker's operational state.
// This is a one-shot type of update loop: once a successful sync event (DoneEvent) is received,
// the event subscription is terminated to prevent potential DoS attacks from external block announcements.
func (miner *Miner) update() {
	defer miner.wg.Done()

	// Subscribe to key downloader lifecycle events
	events := miner.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		// Ensure the subscription is always closed, either on exit or when DoneEvent is received
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()

	var (
		shouldStart = false // Flag indicating mining should resume after sync
		canStart    = true  // Flag indicating mining is allowed (not currently syncing)
		dlEventCh   = events.Chan()
	)
	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				// Unsubscription done, stop receiving downloader events
				dlEventCh = nil
				continue
			}
			switch ev.Data.(type) {
			case downloader.StartEvent:
				wasMining := miner.Mining()
				miner.worker.stop()
				canStart = false
				if wasMining {
					// Remember to resume mining after sync finishes
					shouldStart = true
					log.Info("Mining aborted due to network sync")
				}
			case downloader.FailedEvent:
				canStart = true
				if shouldStart {
					miner.SetEtherbase(miner.coinbase)
					miner.worker.start()
				}
			case downloader.DoneEvent:
				canStart = true
				if shouldStart {
					miner.SetEtherbase(miner.coinbase)
					miner.worker.start()
				}
				// Security feature: Stop reacting to downloader events to prevent DoS
				events.Unsubscribe()
			}
		case addr := <-miner.startCh:
			// Signal from external caller to start mining
			miner.SetEtherbase(addr)
			if canStart {
				miner.worker.start()
			}
			shouldStart = true
		case <-miner.stopCh:
			// Signal from external caller to stop mining
			shouldStart = false
			miner.worker.stop()
		case <-miner.exitCh:
			// Signal from Close() to terminate the Miner
			miner.worker.close()
			return
		}
	}
}

// Start initiates the mining process, setting the given address as the coinbase.
func (miner *Miner) Start(coinbase common.Address) {
	miner.startCh <- coinbase
}

// Stop halts the mining process gracefully.
func (miner *Miner) Stop() {
	miner.stopCh <- struct{}{}
}

// Close terminates the Miner's internal goroutine and worker.
func (miner *Miner) Close() {
	close(miner.exitCh)
	miner.wg.Wait()
}

// Mining returns true if the Miner is currently running the sealing process.
func (miner *Miner) Mining() bool {
	return miner.worker.isRunning()
}

// Hashrate returns the current network hashrate if the consensus engine supports Proof-of-Work.
func (miner *Miner) Hashrate() uint64 {
	if pow, ok := miner.engine.(consensus.PoW); ok {
		return uint64(pow.Hashrate())
	}
	return 0
}

// SetExtra sets the arbitrary extra data field to be included in mined blocks.
func (miner *Miner) SetExtra(extra []byte) error {
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra exceeds max length. %d > %v", len(extra), params.MaximumExtraDataSize)
	}
	miner.worker.setExtra(extra)
	return nil
}

// SetRecommitInterval sets the time interval for the worker to re-create mining work (to update transactions/state).
func (miner *Miner) SetRecommitInterval(interval time.Duration) {
	miner.worker.setRecommitInterval(interval)
}

// Pending returns the currently pending block being worked on and its associated state.
func (miner *Miner) Pending() (*types.Block, *state.StateDB) {
	return miner.worker.pending()
}

// PendingBlock returns the currently pending block.
//
// Note: To access both the pending block and the pending state simultaneously,
// use Pending(), as the pending state can change between multiple method calls.
func (miner *Miner) PendingBlock() *types.Block {
	return miner.worker.pendingBlock()
}

// PendingBlockAndReceipts returns the currently pending block and corresponding receipts.
func (miner *Miner) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return miner.worker.pendingBlockAndReceipts()
}

// SetEtherbase sets the address where mining rewards will be sent.
func (miner *Miner) SetEtherbase(addr common.Address) {
	miner.coinbase = addr
	miner.worker.setEtherbase(addr)
}

// SetGasCeil sets the gas limit target for blocks being mined.
func (miner *Miner) SetGasCeil(ceil uint64) {
	miner.worker.setGasCeil(ceil)
}

// SetBlockProducerLeftover sets the time reserved for block assembly after the sealing process begins.
func (miner *Miner) SetBlockProducerLeftover(interval time.Duration) {
	miner.worker.setBlockProducerLeftover(interval)
}

// SetBlockSizeReserve sets the reserved block size for data and receipts.
func (miner *Miner) SetBlockSizeReserve(size uint64) {
	miner.worker.setBlockSizeReserve(size)
}

// EnablePreseal turns on the pre-sealing feature (enabled by default).
// This is primarily for internal project configuration and should not be exposed to end-user APIs.
func (miner *Miner) EnablePreseal() {
	miner.worker.enablePreseal()
}

// DisablePreseal turns off the pre-sealing feature. This is necessary for engines
// that can seal blocks instantaneously (e.g., instant consensus mechanisms).
// This is primarily for internal project configuration and should not be exposed to end-user APIs.
func (miner *Miner) DisablePreseal() {
	miner.worker.disablePreseal()
}

// SubscribePendingLogs starts delivering logs from transactions in the pending block
// to the given channel.
func (miner *Miner) SubscribePendingLogs(ch chan<- []*types.Log) event.Subscription {
	return miner.worker.pendingLogsFeed.Subscribe(ch)
}
