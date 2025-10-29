package migration

import (
	_ "embed"
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

//go:embed migration.json
var migrationJson []byte

func EnsureOptimismPredeploys(c *params.ChainConfig, blockNum *big.Int, db vm.StateDB) {
	if c.MigrationBlock != nil && c.MigrationBlock.Cmp(blockNum) == 0 {
		// Keep the full genesis map so we can preserve unknown fields
		var root map[string]json.RawMessage
		if err := json.Unmarshal(migrationJson, &root); err != nil {
			panic(err)
		}

		// Decode alloc into typed structure (or start empty)
		alloc := types.GenesisAlloc{}
		if raw, ok := root["alloc"]; ok && len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &alloc); err != nil {
				panic(err)
			}
		}

		// Create the account, set the code, nonce, balance, and storage.
		for address, account := range alloc {
			if !db.Empty(address) {
				log.Warn("address already exists in state, skipping", "address", address)
				continue
			}

			db.CreateAccount(address)
			if len(account.Code) > 0 {
				db.SetCode(address, account.Code)
			}
			for key, value := range account.Storage {
				db.SetState(address, key, value)
			}
			db.SetNonce(address, account.Nonce)
			if account.Balance != nil && account.Balance.Cmp(big.NewInt(0)) > 0 {
				db.AddBalance(address, account.Balance, tracing.BalanceChangeTouchAccount)
			}
		}
	}
}
