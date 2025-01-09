package simapp

import (
	"bytes"
	"context"
	simsxv2 "github.com/cosmos/cosmos-sdk/simsx/v2"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	v2 "github.com/cosmos/cosmos-sdk/x/genutil/v2"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	"github.com/stretchr/testify/require"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func init() {
	simcli.GetSimulatorFlags()
}

func TestFullAppSimulation(t *testing.T) {
	RunWithSeeds[Tx](t, NewSimApp[Tx], AppConfig, defaultSeeds)
}

func TestAppStateDeterminism(t *testing.T) {
	var seeds []int64
	if s := simcli.NewConfigFromFlags().Seed; s != simcli.DefaultSeedValue {
		// override defaults with user data
		seeds = []int64{s, s, s} // run same simulation 3 times
	} else {
		seeds = []int64{ // some random seeds, tripled to ensure same app-hash on all runs
			1, 1, 1,
			3, 3, 3,
			5, 5, 5,
		}
	}

	var mx sync.Mutex
	appHashResults := make(map[int64][]byte)
	captureAndCheckHash := func(t testing.TB, appHash []byte, ti TestInstance[Tx], _ []simtypes.Account) {
		seed := ti.Seed
		mx.Lock()
		defer mx.Unlock()
		otherHashes, ok := appHashResults[seed]
		if !ok {
			appHashResults[seed] = appHash
			return
		}
		if !bytes.Equal(otherHashes, appHash) {
			t.Fatalf("non-determinism in seed %d", seed)
		}
	}
	// run simulations
	RunWithSeeds(t, NewSimApp[Tx], AppConfig, seeds, captureAndCheckHash)
}

type ExportableApp interface {
	ExportAppStateAndValidators(forZeroHeight bool, jailAllowedAddrs []string) (v2.ExportedApp, error)
}

// Scenario:
//
//	Start a fresh node and run n blocks, export state
//	set up a new node instance, Init chain from exported genesis
//	run new instance for n blocks
func TestAppSimulationAfterImport(t *testing.T) {
	appFactory := NewSimApp[Tx]
	exportAndStartChainFromGenesisPostAction := func(t testing.TB, _ []byte, ti TestInstance[Tx], accs []simtypes.Account) {
		t.Log("exporting genesis...\n")
		app, ok := ti.App.(ExportableApp)
		require.True(t, ok)
		exported, err := app.ExportAppStateAndValidators(false, []string{})
		require.NoError(t, err)

		genesisTimestamp := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
		startHeight := uint64(exported.Height + 1)
		seed := ti.Seed

		importGenesisChainStateFactory := func(ctx context.Context, r *rand.Rand) (TestInstance[Tx], ChainState[Tx], []simtypes.Account) {
			testInstance := SetupTestInstance(t, appFactory, AppConfig, seed)
			appManager := testInstance.AppManager
			appStore := testInstance.App.Store()
			txConfig := testInstance.App.TxConfig()
			chainID := SimAppChainID
			initRsp, stateRoot := doChainInitWithGenesis(
				t,
				ctx,
				chainID,
				genesisTimestamp,
				appManager,
				testInstance.TxDecoder,
				exported.AppState,
				appStore,
				startHeight,
			)

			activeValidatorSet := simsxv2.NewValSet().Update(initRsp.ValidatorUpdates)
			valsetHistory := simsxv2.NewValSetHistory(1)
			valsetHistory.Add(genesisTimestamp, activeValidatorSet)
			cs := ChainState[Tx]{
				chainID:            chainID,
				blockTime:          genesisTimestamp,
				activeValidatorSet: activeValidatorSet,
				valsetHistory:      valsetHistory,
				stateRoot:          stateRoot,
				app:                appManager,
				appStore:           appStore,
				txConfig:           txConfig,
			}

			return testInstance, cs, accs
		}
		// run sims with new app setup from exported genesis
		RunWithSeedX[Tx](t, importGenesisChainStateFactory, startHeight, seed)
	}
	RunWithSeeds[Tx, *SimApp[Tx]](t, appFactory, AppConfig, defaultSeeds, exportAndStartChainFromGenesisPostAction)
}
