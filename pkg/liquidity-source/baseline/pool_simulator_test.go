package baseline

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	graphqlpkg "github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/graphql"
)

func skipIfNoEnv(t *testing.T) (rpcURL, graphqlURL, relayAddr string) {
	t.Helper()
	rpcURL = os.Getenv("BASELINE_RPC_URL")
	graphqlURL = os.Getenv("BASELINE_GRAPHQL_URL")
	relayAddr = os.Getenv("BASELINE_RELAY_ADDRESS")
	if rpcURL == "" || graphqlURL == "" || relayAddr == "" {
		t.Skip("Set BASELINE_RPC_URL, BASELINE_GRAPHQL_URL, and BASELINE_RELAY_ADDRESS to run live tests")
	}
	return
}

func TestPoolsListUpdater_GetNewPools(t *testing.T) {
	rpcURL, graphqlURL, relayAddr := skipIfNoEnv(t)

	ethrpcClient := ethrpc.New(rpcURL)
	ethrpcClient.SetMulticallContract(common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))

	graphqlClient := graphqlpkg.NewClient(graphqlURL)

	cfg := &Config{
		DexID:        "baseline",
		ChainID:      84532,
		RelayAddress: relayAddr,
		NewPoolLimit: 10,
	}

	updater := NewPoolsListUpdater(cfg, ethrpcClient, graphqlClient)

	pools, metadata, err := updater.GetNewPools(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetNewPools failed: %v", err)
	}

	t.Logf("Found %d pools", len(pools))
	for _, p := range pools {
		t.Logf("  Pool: %s (%s/%s)", p.Address, p.Tokens[0].Symbol, p.Tokens[1].Symbol)
	}
	t.Logf("Metadata: %s", string(metadata))
}

func TestPoolTracker_GetNewPoolState(t *testing.T) {
	rpcURL, _, relayAddr := skipIfNoEnv(t)

	ethrpcClient := ethrpc.New(rpcURL)
	ethrpcClient.SetMulticallContract(common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))

	cfg := &Config{
		DexID:        "baseline",
		ChainID:      84532,
		RelayAddress: relayAddr,
	}

	tracker, err := NewPoolTracker(cfg, ethrpcClient)
	if err != nil {
		t.Fatalf("NewPoolTracker failed: %v", err)
	}

	testPool := entity.Pool{
		Address:  "0x39eeaf94bb996c5e19ae51eae392a11c5e7b6b84",
		Exchange: "baseline",
		Type:     DexType,
		Reserves: entity.PoolReserves{"0", "0"},
		Tokens: []*entity.PoolToken{
			{Address: "0xb85885897d297000a74ea2e4711c3ca729461abc", Decimals: 18, Symbol: "WETH", Swappable: true},
			{Address: "0x39eeaf94bb996c5e19ae51eae392a11c5e7b6b84", Decimals: 18, Symbol: "TB5", Swappable: true},
		},
	}

	updated, err := tracker.GetNewPoolState(context.Background(), testPool, pool.GetNewPoolStateParams{})
	if err != nil {
		t.Fatalf("GetNewPoolState failed: %v", err)
	}

	t.Logf("Reserves: %v", updated.Reserves)

	var extra Extra
	if err := json.Unmarshal([]byte(updated.Extra), &extra); err != nil {
		t.Fatalf("Failed to unmarshal extra: %v", err)
	}

	if extra.BuyRate[0] != nil {
		t.Logf("Buy rate: %s -> %s", extra.BuyRate[0], extra.BuyRate[1])
	}
	if extra.SellRate[0] != nil {
		t.Logf("Sell rate: %s -> %s", extra.SellRate[0], extra.SellRate[1])
	}
}

func TestPoolSimulator_CalcAmountOut(t *testing.T) {
	rpcURL, _, relayAddr := skipIfNoEnv(t)

	ethrpcClient := ethrpc.New(rpcURL)
	ethrpcClient.SetMulticallContract(common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))

	cfg := &Config{
		DexID:        "baseline",
		ChainID:      84532,
		RelayAddress: relayAddr,
	}

	tracker, err := NewPoolTracker(cfg, ethrpcClient)
	if err != nil {
		t.Fatalf("NewPoolTracker failed: %v", err)
	}

	testPool := entity.Pool{
		Address:  "0x39eeaf94bb996c5e19ae51eae392a11c5e7b6b84",
		Exchange: "baseline",
		Type:     DexType,
		Reserves: entity.PoolReserves{"0", "0"},
		Tokens: []*entity.PoolToken{
			{Address: "0xb85885897d297000a74ea2e4711c3ca729461abc", Decimals: 18, Symbol: "WETH", Swappable: true},
			{Address: "0x39eeaf94bb996c5e19ae51eae392a11c5e7b6b84", Decimals: 18, Symbol: "TB5", Swappable: true},
		},
	}

	updated, err := tracker.GetNewPoolState(context.Background(), testPool, pool.GetNewPoolStateParams{})
	if err != nil {
		t.Fatalf("GetNewPoolState failed: %v", err)
	}

	sim, err := NewPoolSimulator(updated)
	if err != nil {
		t.Fatalf("NewPoolSimulator failed: %v", err)
	}

	// Test buy: 0.01 WETH -> TB5
	result, err := sim.CalcAmountOut(pool.CalcAmountOutParams{
		TokenAmountIn: pool.TokenAmount{
			Token:  "0xb85885897d297000a74ea2e4711c3ca729461abc",
			Amount: big.NewInt(1e16),
		},
		TokenOut: "0x39eeaf94bb996c5e19ae51eae392a11c5e7b6b84",
	})
	if err != nil {
		t.Fatalf("CalcAmountOut (buy) failed: %v", err)
	}
	t.Logf("Buy: 0.01 WETH -> %s TB5", result.TokenAmountOut.Amount)

	// Test sell: 1 TB5 -> WETH
	result, err = sim.CalcAmountOut(pool.CalcAmountOutParams{
		TokenAmountIn: pool.TokenAmount{
			Token:  "0x39eeaf94bb996c5e19ae51eae392a11c5e7b6b84",
			Amount: big.NewInt(1e18),
		},
		TokenOut: "0xb85885897d297000a74ea2e4711c3ca729461abc",
	})
	if err != nil {
		fmt.Printf("CalcAmountOut (sell) failed: %v\n", err)
	} else {
		t.Logf("Sell: 1 TB5 -> %s WETH", result.TokenAmountOut.Amount)
	}
}
