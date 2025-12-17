// Copyright 2015 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/node"
	"github.com/urfave/cli/v2"
)

var (
	initCommand = &cli.Command{
		Action:    initGenesis,
		Name:      "init",
		Usage:     "Bootstrap and initialize a new genesis block",
		ArgsUsage: "<genesisPath>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.DBEngineFlag,
			utils.ForceOverrideChainConfigFlag,
			utils.CachePreimagesFlag,
			utils.StateSchemeFlag,
			utils.AncientFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
The init command initializes a new genesis block and definition for the network.
This is a destructive action and changes the network in which you will be
participating.

It expects the genesis file as argument.`,
	}
	dumpGenesisCommand = &cli.Command{
		Action:    dumpGenesis,
		Name:      "dumpgenesis",
		Usage:     "Dumps genesis block JSON configuration to stdout",
		ArgsUsage: "",
		Flags: []cli.Flag{
			utils.MainnetFlag,
			utils.RopstenFlag,
			utils.SepoliaFlag,
			utils.RinkebyFlag,
			utils.GoerliFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
The dumpgenesis command dumps the genesis block configuration in JSON format to stdout.`,
	}
	importCommand = &cli.Command{
		Action:    importChain,
		Name:      "import",
		Usage:     "Import a blockchain file",
		ArgsUsage: "<filename> (<filename 2> ... <filename N>) ",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.DBEngineFlag,
			utils.CacheFlag,
			utils.SyncModeFlag,
			utils.GCModeFlag,
			utils.NoPruningSideCarFlag,
			utils.SnapshotFlag,
			utils.CacheDatabaseFlag,
			utils.CacheGCFlag,
			utils.MetricsEnabledFlag,
			utils.MetricsEnabledExpensiveFlag,
			utils.MetricsHTTPFlag,
			utils.MetricsPortFlag,
			utils.MetricsEnableInfluxDBFlag,
			utils.MetricsEnableInfluxDBV2Flag,
			utils.MetricsInfluxDBEndpointFlag,
			utils.MetricsInfluxDBDatabaseFlag,
			utils.MetricsInfluxDBUsernameFlag,
			utils.MetricsInfluxDBPasswordFlag,
			utils.MetricsInfluxDBTagsFlag,
			utils.MetricsInfluxDBTokenFlag,
			utils.MetricsInfluxDBBucketFlag,
			utils.MetricsInfluxDBOrganizationFlag,
			utils.TxLookupLimitFlag,
			utils.TransactionHistoryFlag,
			utils.StateSchemeFlag,
			utils.StateHistoryFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
The import command imports blocks from an RLP-encoded form. The form can be one file
with several RLP-encoded blocks, or several files can be used.

If only one file is used, import error will result in failure. If several files are used,
processing will proceed even if an individual RLP-file import failure occurs.`,
	}
	exportCommand = &cli.Command{
		Action:    exportChain,
		Name:      "export",
		Usage:     "Export blockchain into file",
		ArgsUsage: "<filename> [<blockNumFirst> <blockNumLast>]",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.DBEngineFlag,
			utils.CacheFlag,
			utils.SyncModeFlag,
			utils.StateSchemeFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
Requires a first argument of the file to write to.
Optional second and third arguments control the first and
last block to write. In this mode, the file will be appended
if already existing. If the file ends with .gz, the output will
be gzipped.`,
	}
	importPreimagesCommand = &cli.Command{
		Action:    importPreimages,
		Name:      "import-preimages",
		Usage:     "Import the preimage database from an RLP stream",
		ArgsUsage: "<datafile>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.DBEngineFlag,
			utils.CacheFlag,
			utils.SyncModeFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
The import-preimages command imports hash preimages from an RLP encoded stream.
It's deprecated, please use "geth db import" instead.
`,
	}
	exportPreimagesCommand = &cli.Command{
		Action:    exportPreimages,
		Name:      "export-preimages",
		Usage:     "Export the preimage database into an RLP stream",
		ArgsUsage: "<dumpfile>",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.DBEngineFlag,
			utils.CacheFlag,
			utils.SyncModeFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
The export-preimages command exports hash preimages to an RLP encoded stream.
It's deprecated, please use "geth db export" instead.
`,
	}
	dumpCommand = &cli.Command{
		Action:    dump,
		Name:      "dump",
		Usage:     "Dump a specific block from storage",
		ArgsUsage: "[? <blockHash> | <blockNum>]",
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.DBEngineFlag,
			utils.CacheFlag,
			utils.IterativeOutputFlag,
			utils.ExcludeCodeFlag,
			utils.ExcludeStorageFlag,
			utils.IncludeIncompletesFlag,
			utils.StartKeyFlag,
			utils.EndKeyFlag,
			utils.DumpLimitFlag,
			utils.OutPathPrefixFlag,
		},
		Category: "BLOCKCHAIN COMMANDS",
		Description: `
This command dumps out the state for a given block (or latest, if none provided).
`,
	}
)

// initGenesis will initialise the given JSON format genesis file and writes it as
// the zero'd block (i.e. genesis) or will fail hard if it can't succeed.
func initGenesis(ctx *cli.Context) error {
	if ctx.Args().Len() != 1 {
		utils.Fatalf("need genesis.json file as the only argument")
	}
	// Make sure we have a valid genesis JSON
	genesisPath := ctx.Args().First()
	if len(genesisPath) == 0 {
		utils.Fatalf("Must supply path to genesis JSON file")
	}
	file, err := os.Open(genesisPath)
	if err != nil {
		utils.Fatalf("Failed to read genesis file: %v", err)
	}
	defer file.Close()

	genesis := new(core.Genesis)
	if err := json.NewDecoder(file).Decode(genesis); err != nil {
		utils.Fatalf("invalid genesis file: %v", err)
	}
	var overrideChainConfig bool
	if ctx.IsSet(utils.ForceOverrideChainConfigFlag.Name) {
		overrideChainConfig = ctx.Bool(utils.ForceOverrideChainConfigFlag.Name)
	}
	// Open and initialise both full and light databases
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	for _, name := range []string{"chaindata", "lightchaindata"} {
		chaindb, err := stack.OpenDatabaseWithFreezer(name, 0, 0, ctx.String(utils.AncientFlag.Name), "", false)
		if err != nil {
			utils.Fatalf("Failed to open database: %v", err)
		}
		// Create triedb firstly

		triedb := utils.MakeTrieDatabase(ctx, chaindb, ctx.Bool(utils.CachePreimagesFlag.Name), false)
		defer chaindb.Close()
		_, hash, err := core.SetupGenesisBlock(chaindb, triedb, genesis, overrideChainConfig)
		if err != nil {
			utils.Fatalf("Failed to write genesis block: %v", err)
		}
		log.Info("Successfully wrote genesis state", "database", name, "hash", hash)
	}
	return nil
}

func dumpGenesis(ctx *cli.Context) error {
	// TODO(rjl493456442) support loading from the custom datadir
	genesis := utils.MakeGenesis(ctx)
	if genesis == nil {
		genesis = core.DefaultGenesisBlock()
	}
	if err := json.NewEncoder(os.Stdout).Encode(genesis); err != nil {
		utils.Fatalf("could not encode genesis")
	}
	return nil
}

func importChain(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}
	// Start metrics export if enabled
	utils.SetupMetrics(ctx)
	// Start system runtime metrics collection
	go metrics.CollectProcessMetrics(3 * time.Second)

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, db := utils.MakeChain(ctx, stack)
	defer db.Close()

	// Start periodically gathering memory profiles
	var peakMemAlloc, peakMemSys uint64
	go func() {
		stats := new(runtime.MemStats)
		for {
			runtime.ReadMemStats(stats)
			if atomic.LoadUint64(&peakMemAlloc) < stats.Alloc {
				atomic.StoreUint64(&peakMemAlloc, stats.Alloc)
			}
			if atomic.LoadUint64(&peakMemSys) < stats.Sys {
				atomic.StoreUint64(&peakMemSys, stats.Sys)
			}
			time.Sleep(5 * time.Second)
		}
	}()
	// Import the chain
	start := time.Now()

	var importErr error

	if ctx.Args().Len() == 1 {
		if err := utils.ImportChain(chain, ctx.Args().First()); err != nil {
			importErr = err
			log.Error("Import error", "err", err)
		}
	} else {
		for _, arg := range ctx.Args().Slice() {
			if err := utils.ImportChain(chain, arg); err != nil {
				importErr = err
				log.Error("Import error", "file", arg, "err", err)
			}
		}
	}
	chain.Stop()
	fmt.Printf("Import done in %v.\n\n", time.Since(start))

	// Output pre-compaction stats mostly to see the import trashing
	showLeveldbStats(db)

	// Print the memory statistics used by the importing
	mem := new(runtime.MemStats)
	runtime.ReadMemStats(mem)

	fmt.Printf("Object memory: %.3f MB current, %.3f MB peak\n", float64(mem.Alloc)/1024/1024, float64(atomic.LoadUint64(&peakMemAlloc))/1024/1024)
	fmt.Printf("System memory: %.3f MB current, %.3f MB peak\n", float64(mem.Sys)/1024/1024, float64(atomic.LoadUint64(&peakMemSys))/1024/1024)
	fmt.Printf("Allocations:   %.3f million\n", float64(mem.Mallocs)/1000000)
	fmt.Printf("GC pause:      %v\n\n", time.Duration(mem.PauseTotalNs))

	if ctx.Bool(utils.NoCompactionFlag.Name) {
		return nil
	}

	// Compact the entire database to more accurately measure disk io and print the stats
	start = time.Now()
	fmt.Println("Compacting entire database...")
	if err := db.Compact(nil, nil); err != nil {
		utils.Fatalf("Compaction failed: %v", err)
	}
	fmt.Printf("Compaction done in %v.\n\n", time.Since(start))

	showLeveldbStats(db)
	return importErr
}

func exportChain(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	chain, _ := utils.MakeChain(ctx, stack)
	start := time.Now()

	var err error
	fp := ctx.Args().First()
	if ctx.Args().Len() < 3 {
		err = utils.ExportChain(chain, fp)
	} else {
		// This can be improved to allow for numbers larger than 9223372036854775807
		first, ferr := strconv.ParseInt(ctx.Args().Get(1), 10, 64)
		last, lerr := strconv.ParseInt(ctx.Args().Get(2), 10, 64)
		if ferr != nil || lerr != nil {
			utils.Fatalf("Export error in parsing parameters: block number not an integer\n")
		}
		if first < 0 || last < 0 {
			utils.Fatalf("Export error: block number must be greater than 0\n")
		}
		if head := chain.CurrentFastBlock(); uint64(last) > head.NumberU64() {
			utils.Fatalf("Export error: block number %d larger than head block %d\n", uint64(last), head.NumberU64())
		}
		err = utils.ExportAppendChain(chain, fp, uint64(first), uint64(last))
	}

	if err != nil {
		utils.Fatalf("Export error: %v\n", err)
	}
	fmt.Printf("Export done in %v\n", time.Since(start))
	return nil
}

// importPreimages imports preimage data from the specified file.
func importPreimages(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}

	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false)
	start := time.Now()

	if err := utils.ImportPreimages(db, ctx.Args().First()); err != nil {
		utils.Fatalf("Import error: %v\n", err)
	}
	fmt.Printf("Import done in %v\n", time.Since(start))
	return nil
}

// exportPreimages dumps the preimage data to specified json file in streaming way.
func exportPreimages(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		utils.Fatalf("This command requires an argument.")
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	start := time.Now()

	if err := utils.ExportPreimages(db, ctx.Args().First()); err != nil {
		utils.Fatalf("Export error: %v\n", err)
	}
	fmt.Printf("Export done in %v\n", time.Since(start))
	return nil
}

func parseDumpConfig(ctx *cli.Context, stack *node.Node) (*state.DumpConfig, ethdb.Database, common.Hash, error) {
	db := utils.MakeChainDatabase(ctx, stack, true)
	var header *types.Header
	if ctx.NArg() > 1 {
		return nil, nil, common.Hash{}, fmt.Errorf("expected 1 argument (number or hash), got %d", ctx.NArg())
	}
	if ctx.NArg() == 1 {
		arg := ctx.Args().First()
		if hashish(arg) {
			hash := common.HexToHash(arg)
			if number := rawdb.ReadHeaderNumber(db, hash); number != nil {
				header = rawdb.ReadHeader(db, hash, *number)
			} else {
				return nil, nil, common.Hash{}, fmt.Errorf("block %x not found", hash)
			}
		} else {
			number, err := strconv.Atoi(arg)
			if err != nil {
				return nil, nil, common.Hash{}, err
			}
			if hash := rawdb.ReadCanonicalHash(db, uint64(number)); hash != (common.Hash{}) {
				header = rawdb.ReadHeader(db, hash, uint64(number))
			} else {
				return nil, nil, common.Hash{}, fmt.Errorf("header for block %d not found", number)
			}
		}
	} else {
		// Use latest
		header = rawdb.ReadHeadHeader(db)
	}
	if header == nil {
		return nil, nil, common.Hash{}, errors.New("no head block found")
	}
	startArg := common.FromHex(ctx.String(utils.StartKeyFlag.Name))
	var start common.Hash
	switch len(startArg) {
	case 0: // common.Hash
	case 32:
		start = common.BytesToHash(startArg)
	case 20:
		start = crypto.Keccak256Hash(startArg)
		log.Info("Converting start-address to hash", "address", common.BytesToAddress(startArg), "hash", start.Hex())
	default:
		return nil, nil, common.Hash{}, fmt.Errorf("invalid start argument: %x. 20 or 32 hex-encoded bytes required", startArg)
	}

	endArg := common.FromHex(ctx.String(utils.EndKeyFlag.Name))
	var end common.Hash
	switch len(endArg) {
	case 0: // common.Hash
	case 32:
		end = common.BytesToHash(endArg)
	case 20:
		end = crypto.Keccak256Hash(endArg)
		log.Info("Converting end-address to hash", "address", common.BytesToAddress(endArg), "hash", start.Hex())
	default:
		return nil, nil, common.Hash{}, fmt.Errorf("invalid start argument: %x. 20 or 32 hex-encoded bytes required", startArg)
	}
	var conf = &state.DumpConfig{
		SkipCode:          ctx.Bool(utils.ExcludeCodeFlag.Name),
		SkipStorage:       ctx.Bool(utils.ExcludeStorageFlag.Name),
		OnlyWithAddresses: !ctx.Bool(utils.IncludeIncompletesFlag.Name),
		Start:             start.Bytes(),
		End:               end.Bytes(),
		Max:               ctx.Uint64(utils.DumpLimitFlag.Name),
		OutPathPrefix:     ctx.String(utils.OutPathPrefixFlag.Name),
	}
	log.Info("State dump configured", "block", header.Number, "hash", header.Hash().Hex(),
		"skipcode", conf.SkipCode, "skipstorage", conf.SkipStorage,
		"start", hexutil.Encode(conf.Start), "end", hexutil.Encode(conf.End), "limit", conf.Max)
	return conf, db, header.Root, nil
}

func dump(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	conf, db, root, err := parseDumpConfig(ctx, stack)
	if err != nil {
		return err
	}

	keys := [][]byte{
		common.Address{}.Bytes(),
		common.Hex2Bytes("1000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("2000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("20ac36e3ff928c5e1b0ec52fdda044d2cc4508d4c3bc94395ce0282f8196bc21"),
		common.Hex2Bytes("20ac3801b39bc0bf6239baf0a75697858bbc9324754be41e82ec5a046a566053"),
		common.Hex2Bytes("2186419dc4279e765bd3ebf65afe5c07a96d1940df13c98161ed402d28e286b4"),
		common.Hex2Bytes("218641eccf86d538c070872fc07653cd7884d08e383912a9e2484e3f994a083e"),
		common.Hex2Bytes("220129d050aaa9fc2fb9fa23b670177308ccd9e8515fc3b2ff6b605b9821aaf1"),
		common.Hex2Bytes("22012b29b5bd2d1cc7704eb2046cf849f29700c4596f70211881d672cf1453b1"),
		common.Hex2Bytes("2352f3d2143356e01b079add70829f3cb19aab8cd976bf79acdc5a5dc219a428"),
		common.Hex2Bytes("2352f4fbf772538cd9ebc96eb794dd156940c0fd66088ca3a25011ab388f2267"),
		common.Hex2Bytes("3000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("3400000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("3800000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("3b00000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("3f00000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("4000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("5000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("6000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("7000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("8000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("9000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("a000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("a414ad64f6634861d0a743461d403d6da9a63500ab7778a73b557353b8c0cf9f"),
		common.Hex2Bytes("a414af13bc86e1781a1a3efd544575dada471099683e808e810d069f0dfa6b2b"),
		common.Hex2Bytes("a46ec27ca40e9c35fcaa49cbc7d5e53cc8b35e54ab6468b621f29c78aa06a3e7"),
		common.Hex2Bytes("a46ec3cfdee1f1e7039c93f88b9f195b7416704abc46f5fb8a5d37c103fb3f50"),
		common.Hex2Bytes("a59985ee66a204dcbafe814cf858cea34ccaaa8f5911b449fa91bb5418b4d02f"),
		common.Hex2Bytes("a59986cb6c9ddfa35a2c31a4e3e2b89e3ea3d8f94d74dc2469d0db7a353c5cca"),
		common.Hex2Bytes("a6bc165c28a125f97ff92ab91a76c250e34011153bfd2deb42cddf2da5d23c03"),
		common.Hex2Bytes("a6ca89ec0b0cc2d524bd88a6dee3609f9210f908c8527e45e391119d8265fab2"),
		common.Hex2Bytes("b000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("c000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("d000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("e000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("f000000000000000000000000000000000000000000000000000000000000000"),
	}

	wg := sync.WaitGroup{}
	for i, key := range keys {
		wg.Add(1)
		go func() {
			defer func() {
				wg.Done()
			}()

			newConf := &state.DumpConfig{
				SkipCode:          conf.SkipCode,
				SkipStorage:       conf.SkipStorage,
				OnlyWithAddresses: conf.OnlyWithAddresses,
				Start:             conf.Start,
				OutPathPrefix:     conf.OutPathPrefix,
			}
			newConf.Index = i
			newConf.Start = key
			if i < len(keys)-1 {
				newConf.End = keys[i+1]
			} else {
				newConf.End = nil
			}

			triedb := utils.MakeTrieDatabase(ctx, db, true, true) // always enable preimage lookup
			defer triedb.Close()
			state, err := state.New(root, state.NewDatabaseWithNodeDB(db, triedb), nil)
			if err != nil {
				panic(err)
			}
			if ctx.Bool(utils.IterativeOutputFlag.Name) {
				state.IterativeDump(newConf)
			} else {
				if conf.OnlyWithAddresses {
					fmt.Fprintf(os.Stderr, "If you want to include accounts with missing preimages, you need iterative output, since"+
						" otherwise the accounts will overwrite each other in the resulting mapping.")
					panic(fmt.Errorf("incompatible options"))
				}
				fmt.Println(string(state.Dump(conf)))
			}
		}()

	}

	wg.Wait()

	return nil
}

// hashish returns true for strings that look like hashes.
func hashish(x string) bool {
	_, err := strconv.Atoi(x)
	return err != nil
}
