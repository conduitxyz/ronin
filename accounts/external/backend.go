// Copyright (c) 2019 The go-ethereum Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package external

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// ExternalBackend implements the accounts.Backend interface for interacting with an external signer (e.g., Clef).
type ExternalBackend struct {
	signers []accounts.Wallet
}

func (eb *ExternalBackend) Wallets() []accounts.Wallet {
	return eb.signers
}

// NewExternalBackend initializes a new ExternalBackend connected to the specified RPC endpoint.
func NewExternalBackend(endpoint string) (*ExternalBackend, error) {
	signer, err := NewExternalSigner(endpoint)
	if err != nil {
		return nil, err
	}
	return &ExternalBackend{
		signers: []accounts.Wallet{signer},
	}, nil
}

// Subscribe provides a dummy subscription as the external signer does not emit real-time events.
func (eb *ExternalBackend) Subscribe(sink chan<- accounts.WalletEvent) event.Subscription {
	return event.NewSubscription(func(quit <-chan struct{}) error {
		// Wait indefinitely until the quit signal is received
		<-quit
		return nil
	})
}

// ExternalSigner provides an API to interact with an external signer (Clef).
// It proxies requests to the external signer while handling connection status and account caching.
type ExternalSigner struct {
	client   *rpc.Client
	endpoint string
	status   string
	cacheMu  sync.RWMutex
	cache    []accounts.Account
	lastPing time.Time // Optimization: Track last successful ping time
}

// NewExternalSigner establishes the RPC connection and verifies reachability by pinging the version.
func NewExternalSigner(endpoint string) (*ExternalSigner, error) {
	client, err := rpc.Dial(endpoint)
	if err != nil {
		return nil, err
	}
	extsigner := &ExternalSigner{
		client:   client,
		endpoint: endpoint,
	}
	
	// Check if reachable and retrieve version
	version, err := extsigner.pingVersion()
	if err != nil {
		return nil, fmt.Errorf("external signer ping failed: %w", err)
	}
	extsigner.status = fmt.Sprintf("ok [version=%v]", version)
	extsigner.lastPing = time.Now()
	
	return extsigner, nil
}

func (api *ExternalSigner) URL() accounts.URL {
	return accounts.URL{
		Scheme: "extapi",
		Path:   api.endpoint,
	}
}

func (api *ExternalSigner) Status() (string, error) {
	// Optimization: Add a simple TTL cache for status checks to avoid RPC spam
	api.cacheMu.RLock()
	isStale := time.Since(api.lastPing) > 5*time.Minute
	status := api.status
	api.cacheMu.RUnlock()
	
	if isStale {
		version, err := api.pingVersion()
		if err == nil {
			api.cacheMu.Lock()
			api.status = fmt.Sprintf("ok [version=%v]", version)
			api.lastPing = time.Now()
			status = api.status
			api.cacheMu.Unlock()
		}
	}
	
	return status, nil
}

// Open and Close operations are not supported by the external signer backend.
func (api *ExternalSigner) Open(passphrase string) error {
	return fmt.Errorf("operation not supported on external signers")
}

func (api *ExternalSigner) Close() error {
	return fmt.Errorf("operation not supported on external signers")
}

// Accounts fetches the list of accounts managed by the external signer and caches them.
func (api *ExternalSigner) Accounts() []accounts.Account {
	var accnts []accounts.Account
	res, err := api.listAccounts()
	if err != nil {
		log.Error("account listing failed", "error", err, "endpoint", api.endpoint)
		return accnts // Returns empty slice on error
	}
	
	for _, addr := range res {
		accnts = append(accnts, accounts.Account{
			URL:     api.URL(), // Use the Wallet's URL structure
			Address: addr,
		})
	}
	
	api.cacheMu.Lock()
	api.cache = accnts
	api.cacheMu.Unlock()
	
	return accnts
}

// Contains checks if the external signer manages the given account.
func (api *ExternalSigner) Contains(account accounts.Account) bool {
	api.cacheMu.RLock()
	defer api.cacheMu.RUnlock()
	
	// If the cache is empty, explicitly unlock the read lock, populate, and relock.
	if api.cache == nil {
		api.cacheMu.RUnlock()
		api.Accounts() // This handles its own locking
		api.cacheMu.RLock()
	}
	
	for _, a := range api.cache {
		// Check address match AND (URL is empty OR URL matches the signer's URL)
		if a.Address == account.Address && (account.URL == (accounts.URL{}) || account.URL == api.URL()) {
			return true
		}
	}
	return false
}

// Derive is not supported on external signers.
func (api *ExternalSigner) Derive(path accounts.DerivationPath, pin bool) (accounts.Account, error) {
	return accounts.Account{}, fmt.Errorf("operation not supported on external signers")
}

// SelfDerive is not supported on external signers.
func (api *ExternalSigner) SelfDerive(bases []accounts.DerivationPath, chain ethereum.ChainStateReader) {
	log.Error("operation SelfDerive not supported on external signers", "endpoint", api.endpoint)
}

// signHash is not supported on external signers.
func (api *ExternalSigner) signHash(account accounts.Account, hash []byte) ([]byte, error) {
	return []byte{}, fmt.Errorf("operation not supported on external signers")
}

// SignData signs keccak256(data). The mimetype parameter describes the type of data being signed.
func (api *ExternalSigner) SignData(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
	var res hexutil.Bytes
	// MixedcaseAddress is required for correct MarshalJSON behavior on Clef RPC interface
	signAddress := common.NewMixedcaseAddress(account.Address) 
	
	if err := api.client.Call(&res, "account_signData",
		mimeType,
		&signAddress, 
		hexutil.Encode(data)); err != nil {
		return nil, err
	}
	
	// FIX: Ensure V value is correctly transformed for Clique/PoA protocols if necessary.
	if mimeType == accounts.MimetypeClique && (res[64] == 27 || res[64] == 28) {
		res[64] -= 27 // Transform V from 27/28 to 0/1
	}
	return res, nil
}

// SignText signs the provided text according to EIP-191.
func (api *ExternalSigner) SignText(account accounts.Account, text []byte) ([]byte, error) {
	var signature hexutil.Bytes
	signAddress := common.NewMixedcaseAddress(account.Address)
	
	if err := api.client.Call(&signature, "account_signData",
		accounts.MimetypeTextPlain,
		&signAddress, 
		hexutil.Encode(text)); err != nil {
		return nil, err
	}
	
	// Transform V from 27/28 (Ethereum-legacy) to 0/1 if clef has not already done so.
	if signature[64] == 27 || signature[64] == 28 {
		signature[64] -= 27 
	}
	return signature, nil
}

// signTransactionResult represents the signing result returned by clef.
type signTransactionResult struct {
	Raw hexutil.Bytes     `json:"raw"`
	Tx  *types.Transaction `json:"tx"`
}

// SignTx sends the transaction to the external signer for signing.
// It handles chainID precedence and maps transaction types (Legacy, EIP-2930, EIP-1559, etc.) 
// to the appropriate Clef arguments.
func (api *ExternalSigner) SignTx(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	
	// Prepare common transaction arguments
	var to *common.MixedcaseAddress
	if tx.To() != nil {
		t := common.NewMixedcaseAddress(*tx.To())
		to = &t
	}
	
	args := &apitypes.SendTxArgs{
		Data:  (*hexutil.Bytes)(tx.Data()), // Directly use tx.Data() instead of redundant local variable
		Nonce: hexutil.Uint64(tx.Nonce()),
		Value: hexutil.Big(*tx.Value()),
		Gas:   hexutil.Uint64(tx.Gas()),
		To:    to,
		From:  common.NewMixedcaseAddress(account.Address),
	}
	
	// Map Gas/Fee parameters based on Tx Type
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType:
		args.GasPrice = (*hexutil.Big)(tx.GasPrice())
	case types.DynamicFeeTxType, types.BlobTxType, types.SetCodeTxType:
		args.MaxFeePerGas = (*hexutil.Big)(tx.GasFeeCap())
		args.MaxPriorityFeePerGas = (*hexutil.Big)(tx.GasTipCap())
	default:
		return nil, fmt.Errorf("unsupported tx type %d", tx.Type())
	}
	
	// --- ChainID Precedence Logic ---
	
	// 1. Prioritize chainID provided as function argument (typically the operating chain)
	if chainID != nil && chainID.Sign() != 0 {
		args.ChainID = (*hexutil.Big)(chainID)
	}
	
	// 2. If the transaction itself has a non-zero ChainID, it overrides the function argument
	//    (Crucial for EIP-155 compatibility and non-legacy tx types)
	if tx.ChainId().Sign() != 0 {
		args.ChainID = (*hexutil.Big)(tx.ChainId())
	}
	
	// 3. Include AccessList for relevant transaction types
	if tx.Type() == types.AccessListTxType || tx.Type() == types.DynamicFeeTxType || tx.Type() == types.BlobTxType {
		accessList := tx.AccessList()
		args.AccessList = &accessList
	}


	var res signTransactionResult
	if err := api.client.Call(&res, "account_signTransaction", args); err != nil {
		return nil, err
	}
	
	// Return the signed transaction object
	return res.Tx, nil
}

// Password operations are not supported by the external signer backend.
func (api *ExternalSigner) SignTextWithPassphrase(account accounts.Account, passphrase string, text []byte) ([]byte, error) {
	return []byte{}, fmt.Errorf("password-operations not supported on external signers")
}

func (api *ExternalSigner) SignTxWithPassphrase(account accounts.Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("password-operations not supported on external signers")
}
func (api *ExternalSigner) SignDataWithPassphrase(account accounts.Account, passphrase, mimeType string, data []byte) ([]byte, error) {
	return nil, fmt.Errorf("password-operations not supported on external signers")
}

// listAccounts proxies the request to the external signer to get all managed addresses.
func (api *ExternalSigner) listAccounts() ([]common.Address, error) {
	var res []common.Address
	if err := api.client.Call(&res, "account_list"); err != nil {
		return nil, err
	}
	return res, nil
}

// pingVersion retrieves the version string from the external signer.
func (api *ExternalSigner) pingVersion() (string, error) {
	var v string
	if err := api.client.Call(&v, "account_version"); err != nil {
		return "", err
	}
	return v, nil
}
