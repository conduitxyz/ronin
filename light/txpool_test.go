// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
// ... (License text kept short for brevity) ...

package light

import (
	"context"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

// Constants defining the scale of the test.
const (
	poolTestTxs    = 1000
	poolTestBlocks = 100
)

// testTxRelay mocks the transaction relay mechanism for testing purposes.
// It captures events related to transaction sending, mining, and discarding.
type testTxRelay struct {
	send    chan int
	discard chan int
	mined   chan int
}

// newTestTxRelay creates a simplified relay with buffered channels to prevent immediate blocking.
func newTestTxRelay() *testTxRelay {
	return &testTxRelay{
		send:    make(chan int, 1),
		discard: make(chan int, 1),
		mined:   make(chan int, 1),
	}
}

// Send implements the relay interface. It signals the number of transactions sent.
// Renamed receiver 'self' to 'r' to follow Go idioms.
func (r *testTxRelay) Send(txs types.Transactions) {
	r.send <- len(txs)
}

// NewHead implements the relay interface. It signals when new blocks are mined.
func (r *testTxRelay) NewHead(head common.Hash, mined []common.Hash, rollback []common.Hash) {
	m := len(mined)
	if m != 0 {
		r.mined <- m
	}
}

// Discard implements the relay interface. It signals the number of discarded transactions.
func (r *testTxRelay) Discard(hashes []common.Hash) {
	r.discard <- len(hashes)
}

// TestTxPool validates the Light Client Transaction Pool logic.
// It simulates a chain where transactions are gradually sent, mined, and eventually discarded.
func TestTxPool(t *testing.T) {
	// Define helper functions to calculate transaction flow curves.
	// Using closure to keep logic contained within the test scope.
	
	// sentTx calculates the total transactions sent before block i.
	sentTx := func(i int) int {
		return int(math.Pow(float64(i)/float64(poolTestBlocks), 0.9) * float64(poolTestTxs))
	}

	// minedTx calculates the total transactions included in block i or before.
	minedTx := func(i int) int {
		return int(math.Pow(float64(i)/float64(poolTestBlocks), 1.1) * float64(poolTestTxs))
	}

	// Initialize test transactions.
	// This replaces the global 'var testTx' to ensure test isolation.
	testTxs := make([]*types.Transaction, poolTestTxs)
	for i := range testTxs {
		tx := types.NewTransaction(
			uint64(i),
			acc1Addr,
			big.NewInt(10000),
			params.TxGas,
			big.NewInt(params.InitialBaseFee),
			nil,
		)
		signedTx, err := types.SignTx(tx, types.HomesteadSigner{}, testBankKey)
		if err != nil {
			t.Fatalf("failed to sign transaction %d: %v", i, err)
		}
		testTxs[i] = signedTx
	}

	// Chain generator function using the local testTxs slice (via closure).
	chainGen := func(i int, block *core.BlockGen) {
		s := minedTx(i)
		e := minedTx(i + 1)
		
		// Boundary checks to prevent index out of range panics
		if s >= len(testTxs) { s = len(testTxs) }
		if e > len(testTxs) { e = len(testTxs) }

		for idx := s; idx < e; idx++ {
			block.AddTx(testTxs[idx])
		}
	}

	// Initialize in-memory databases for server (sdb) and light client (ldb).
	var (
		sdb   = rawdb.NewMemoryDatabase()
		ldb   = rawdb.NewMemoryDatabase()
		gspec = core.Genesis{
			Config:  params.TestChainConfig,
			Alloc:   types.GenesisAlloc{testBankAddress: {Balance: testBankFunds}},
			BaseFee: big.NewInt(params.InitialBaseFee),
		}
	)

	// Commit genesis block to server DB.
	genesis := gspec.MustCommit(sdb, trie.NewDatabase(sdb, nil))
	
	// Commit genesis block to light DB.
	gspec.MustCommit(ldb, trie.NewDatabase(ldb, nil))

	// Assemble the blockchain environment.
	// Using t.Fatal instead of panic for better test failure reporting.
	blockchain, err := core.NewBlockChain(sdb, nil, &gspec, nil, ethash.NewFullFaker(), vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}

	// Generate the test chain using the generator.
	gchain, err := core.GenerateChain(params.TestChainConfig, genesis, ethash.NewFaker(), sdb, poolTestBlocks, chainGen, true)
	if err != nil {
		t.Fatalf("failed to generate chain: %v", err)
	}

	if _, err := blockchain.InsertChain(gchain, nil); err != nil {
		t.Fatalf("failed to insert chain: %v", err)
	}

	// Setup Light Client ODR (On-Demand Retrieval) and Relay.
	odr := &testOdr{sdb: sdb, ldb: ldb, indexerConfig: TestClientIndexerConfig}
	relay := newTestTxRelay()

	lightchain, err := NewLightChain(odr, params.TestChainConfig, ethash.NewFullFaker(), nil)
	if err != nil {
		t.Fatalf("failed to create lightchain: %v", err)
	}

	// NOTE: 'txPermanent' seems to be a package-level variable in the 'light' package.
	// Modifying global state in tests is risky but maintained here to preserve test logic.
	txPermanent = 50 

	pool := NewTxPool(params.TestChainConfig, lightchain, relay)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // Increased timeout slightly for safety
	defer cancel()

	// Main Test Loop: Simulate block-by-block processing
	for ii, block := range gchain {
		i := ii + 1 // logical block number (1-based)
		
		// 1. Simulate users sending transactions to the pool
		s := sentTx(i - 1)
		e := sentTx(i)
		for idx := s; idx < e; idx++ {
			pool.Add(ctx, testTxs[idx])
			
			// Verify relay received the transaction
			got := <-relay.send
			exp := 1
			if got != exp {
				t.Errorf("block %d: relay.Send expected len = %d, got %d", i, exp, got)
			}
		}

		// 2. Import the block header into the light chain
		if _, err := lightchain.InsertHeaderChain([]*types.Header{block.Header()}, 1); err != nil {
			t.Fatalf("block %d: failed to insert header: %v", i, err)
		}

		// 3. Verify that the relay was notified of mined transactions
		got := <-relay.mined
		exp := minedTx(i) - minedTx(i-1)
		if got != exp {
			t.Errorf("block %d: relay.NewHead expected len(mined) = %d, got %d", i, exp, got)
		}

		// 4. Verify discards (transactions removed from pool after confirmation depth)
		exp = 0
		if i > int(txPermanent)+1 {
			exp = minedTx(i-int(txPermanent)-1) - minedTx(i-int(txPermanent)-2)
		}
		
		if exp != 0 {
			got = <-relay.discard
			if got != exp {
				t.Errorf("block %d: relay.Discard expected len = %d, got %d", i, exp, got)
			}
		}
	}
}
