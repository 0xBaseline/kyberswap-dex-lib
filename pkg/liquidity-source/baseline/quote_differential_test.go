package baseline

import (
	"context"
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
)

type baselineDifferentialEnv struct {
	rpcURL      string
	relay       string
	bToken      string
	reserve     string
	blockNumber *big.Int
}

func loadBaselineDifferentialEnv(t *testing.T) baselineDifferentialEnv {
	t.Helper()

	env := baselineDifferentialEnv{
		rpcURL:  os.Getenv("BASELINE_RPC_URL"),
		relay:   os.Getenv("BASELINE_RELAY_ADDRESS"),
		bToken:  os.Getenv("BASELINE_BTOKEN_ADDRESS"),
		reserve: os.Getenv("BASELINE_RESERVE_ADDRESS"),
	}
	if env.rpcURL == "" || env.relay == "" || env.bToken == "" || env.reserve == "" {
		t.Skip("Set BASELINE_RPC_URL, BASELINE_RELAY_ADDRESS, BASELINE_BTOKEN_ADDRESS, and BASELINE_RESERVE_ADDRESS to run Baseline quote differential tests")
	}

	if rawBlock := os.Getenv("BASELINE_BLOCK_NUMBER"); rawBlock != "" {
		blockNumber, ok := new(big.Int).SetString(rawBlock, 10)
		if !ok {
			t.Fatalf("invalid BASELINE_BLOCK_NUMBER: %q", rawBlock)
		}
		env.blockNumber = blockNumber
	}

	return env
}

func newBaselineDifferentialClient(env baselineDifferentialEnv) *ethrpc.Client {
	return ethrpc.New(env.rpcURL)
}

func TestBaselineQuoteDifferential_ExactIn(t *testing.T) {
	env := loadBaselineDifferentialEnv(t)
	ethrpcClient := newBaselineDifferentialClient(env)
	ctx := context.Background()

	state := fetchDifferentialQuoteState(t, ctx, ethrpcClient, env)
	sim := newDifferentialSimulator(t, env, state)

	t.Run("quoteBuyExactIn", func(t *testing.T) {
		for _, amountIn := range reserveExactInAmounts(state) {
			t.Run(amountIn.String(), func(t *testing.T) {
				solidity := callQuoteBuyExactIn(t, ctx, ethrpcClient, env, amountIn)
				goQuote, err := sim.CalcAmountOut(pool.CalcAmountOutParams{
					TokenAmountIn: pool.TokenAmount{Token: env.reserve, Amount: amountIn},
					TokenOut:      env.bToken,
				})
				if err != nil {
					t.Fatalf("Go quoteBuyExactIn failed: %v", err)
				}
				assertBigEqual(t, "tokensOut", solidity.amount, goQuote.TokenAmountOut.Amount)
				assertBigEqual(t, "feesReceived", solidity.fee, goQuote.Fee.Amount)
			})
		}
	})

	t.Run("quoteSellExactIn", func(t *testing.T) {
		for _, amountIn := range sellExactInAmounts(state) {
			t.Run(amountIn.String(), func(t *testing.T) {
				solidity := callQuoteSellExactIn(t, ctx, ethrpcClient, env, amountIn)
				goQuote, err := sim.CalcAmountOut(pool.CalcAmountOutParams{
					TokenAmountIn: pool.TokenAmount{Token: env.bToken, Amount: amountIn},
					TokenOut:      env.reserve,
				})
				if err != nil {
					t.Fatalf("Go quoteSellExactIn failed: %v", err)
				}
				assertBigEqual(t, "amountOut", solidity.amount, goQuote.TokenAmountOut.Amount)
				assertBigEqual(t, "feesReceived", solidity.fee, goQuote.Fee.Amount)
			})
		}
	})
}

func TestBaselineQuoteDifferential_ExactOut(t *testing.T) {
	env := loadBaselineDifferentialEnv(t)
	ethrpcClient := newBaselineDifferentialClient(env)
	ctx := context.Background()

	state := fetchDifferentialQuoteState(t, ctx, ethrpcClient, env)

	t.Run("quoteBuyExactOut", func(t *testing.T) {
		for _, amountOut := range buyExactOutAmounts(state) {
			t.Run(amountOut.String(), func(t *testing.T) {
				solidity := callQuoteBuyExactOut(t, ctx, ethrpcClient, env, amountOut)
				goAmountIn, goFee, err := quoteBuyExactOutCost(cloneQuoteState(state), amountOut)
				if err != nil {
					t.Fatalf("Go quoteBuyExactOut failed: %v", err)
				}
				assertBigEqual(t, "amountIn", solidity.amount, goAmountIn)
				assertBigEqual(t, "feesReceived", solidity.fee, goFee)
			})
		}
	})

	t.Run("quoteSellExactOut", func(t *testing.T) {
		for _, reservesOut := range sellExactOutAmounts(state) {
			t.Run(reservesOut.String(), func(t *testing.T) {
				solidity := callQuoteSellExactOut(t, ctx, ethrpcClient, env, reservesOut)
				goAmountIn, goFee, err := solveSellExactOutForTest(cloneQuoteState(state), reservesOut)
				if err != nil {
					t.Fatalf("Go quoteSellExactOut failed: %v", err)
				}
				assertBigEqual(t, "tokensIn", solidity.amount, goAmountIn)
				assertBigEqual(t, "feesReceived", solidity.fee, goFee)
			})
		}
	})
}

type contractQuote struct {
	amount *big.Int
	fee    *big.Int
}

func fetchDifferentialQuoteState(
	t *testing.T,
	ctx context.Context,
	ethrpcClient *ethrpc.Client,
	env baselineDifferentialEnv,
) *QuoteState {
	t.Helper()

	var result rpcGetQuoteStateResult
	req := ethrpcClient.NewRequest().SetContext(ctx)
	if env.blockNumber != nil {
		req.SetBlockNumber(env.blockNumber)
	}
	req.AddCall(&ethrpc.Call{
		ABI:    relayABI,
		Target: env.relay,
		Method: methodGetQuoteState,
		Params: []any{common.HexToAddress(env.bToken)},
	}, []any{&result})

	if _, err := req.Call(); err != nil {
		t.Fatalf("getQuoteState failed: %v", err)
	}

	return result.State.toQuoteState()
}

func callQuoteBuyExactIn(
	t *testing.T,
	ctx context.Context,
	ethrpcClient *ethrpc.Client,
	env baselineDifferentialEnv,
	amountIn *big.Int,
) contractQuote {
	t.Helper()

	var quote struct{ TokensOut, FeesReceived, Slippage *big.Int }
	callDifferentialQuote(t, ctx, ethrpcClient, env, methodQuoteBuyExactIn, amountIn, &quote)
	return contractQuote{amount: nonNilBI(quote.TokensOut), fee: nonNilBI(quote.FeesReceived)}
}

func callQuoteBuyExactOut(
	t *testing.T,
	ctx context.Context,
	ethrpcClient *ethrpc.Client,
	env baselineDifferentialEnv,
	amountOut *big.Int,
) contractQuote {
	t.Helper()

	var quote struct{ AmountIn, FeesReceived, Slippage *big.Int }
	callDifferentialQuote(t, ctx, ethrpcClient, env, methodQuoteBuyExactOut, amountOut, &quote)
	return contractQuote{amount: nonNilBI(quote.AmountIn), fee: nonNilBI(quote.FeesReceived)}
}

func callQuoteSellExactIn(
	t *testing.T,
	ctx context.Context,
	ethrpcClient *ethrpc.Client,
	env baselineDifferentialEnv,
	amountIn *big.Int,
) contractQuote {
	t.Helper()

	var quote struct{ AmountOut, FeesReceived, Slippage *big.Int }
	callDifferentialQuote(t, ctx, ethrpcClient, env, methodQuoteSellExactIn, amountIn, &quote)
	return contractQuote{amount: nonNilBI(quote.AmountOut), fee: nonNilBI(quote.FeesReceived)}
}

func callQuoteSellExactOut(
	t *testing.T,
	ctx context.Context,
	ethrpcClient *ethrpc.Client,
	env baselineDifferentialEnv,
	reservesOut *big.Int,
) contractQuote {
	t.Helper()

	var quote struct{ TokensIn, FeesReceived, Slippage *big.Int }
	callDifferentialQuote(t, ctx, ethrpcClient, env, methodQuoteSellExactOut, reservesOut, &quote)
	return contractQuote{amount: nonNilBI(quote.TokensIn), fee: nonNilBI(quote.FeesReceived)}
}

func callDifferentialQuote(
	t *testing.T,
	ctx context.Context,
	ethrpcClient *ethrpc.Client,
	env baselineDifferentialEnv,
	method string,
	amount *big.Int,
	output any,
) {
	t.Helper()

	req := ethrpcClient.NewRequest().SetContext(ctx)
	if env.blockNumber != nil {
		req.SetBlockNumber(env.blockNumber)
	}
	req.AddCall(&ethrpc.Call{
		ABI:    relayABI,
		Target: env.relay,
		Method: method,
		Params: []any{common.HexToAddress(env.bToken), amount},
	}, []any{output})

	if _, err := req.Call(); err != nil {
		t.Fatalf("%s(%s) failed: %v", method, amount, err)
	}
}

func newDifferentialSimulator(t *testing.T, env baselineDifferentialEnv, state *QuoteState) *PoolSimulator {
	t.Helper()

	extraBytes, err := json.Marshal(Extra{
		RelayAddress: env.relay,
		QuoteState:   state,
	})
	if err != nil {
		t.Fatalf("marshal extra: %v", err)
	}

	sim, err := NewPoolSimulator(entity.Pool{
		Address:  env.bToken,
		Exchange: "baseline",
		Type:     DexType,
		Reserves: entity.PoolReserves{
			uToBI(state.TotalReserves).String(),
			uToBI(state.TotalBTokens).String(),
		},
		Tokens: []*entity.PoolToken{
			{Address: env.reserve, Decimals: state.ReserveDecimals, Swappable: true},
			{Address: env.bToken, Decimals: bTokenDecimals, Swappable: true},
		},
		Extra: string(extraBytes),
	})
	if err != nil {
		t.Fatalf("NewPoolSimulator failed: %v", err)
	}
	return sim
}

func reserveExactInAmounts(state *QuoteState) []*big.Int {
	unit := decimalUnit(state.ReserveDecimals)
	return uniquePositiveAmounts(
		big.NewInt(1),
		unit,
		divBI(uToBI(state.TotalReserves), big.NewInt(1_000_000)),
		divBI(uToBI(state.TotalReserves), big.NewInt(10_000)),
		divBI(uToBI(state.TotalReserves), big.NewInt(1_000)),
	)
}

func sellExactInAmounts(state *QuoteState) []*big.Int {
	maxSell := uToBI(state.MaxSellDelta)
	return uniquePositiveAmounts(
		big.NewInt(1),
		decimalUnit(bTokenDecimals),
		divBI(maxSell, big.NewInt(10_000)),
		divBI(maxSell, big.NewInt(1_000)),
		divBI(maxSell, big.NewInt(100)),
	)
}

func buyExactOutAmounts(state *QuoteState) []*big.Int {
	totalBTokens := uToBI(state.TotalBTokens)
	return uniquePositiveAmounts(
		big.NewInt(1),
		decimalUnit(bTokenDecimals),
		divBI(totalBTokens, big.NewInt(10_000)),
		divBI(totalBTokens, big.NewInt(1_000)),
		divBI(totalBTokens, big.NewInt(100)),
	)
}

func sellExactOutAmounts(state *QuoteState) []*big.Int {
	totalReserves := uToBI(state.TotalReserves)
	return uniquePositiveAmounts(
		big.NewInt(1),
		decimalUnit(state.ReserveDecimals),
		divBI(totalReserves, big.NewInt(1_000_000)),
		divBI(totalReserves, big.NewInt(10_000)),
		divBI(totalReserves, big.NewInt(1_000)),
	)
}

func uniquePositiveAmounts(candidates ...*big.Int) []*big.Int {
	amounts := make([]*big.Int, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Sign() <= 0 {
			continue
		}
		key := candidate.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		amounts = append(amounts, new(big.Int).Set(candidate))
	}
	return amounts
}

func decimalUnit(decimals uint8) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
}

func solveSellExactOutForTest(state *QuoteState, targetReservesOut *big.Int) (tokensIn, fee *big.Int, err error) {
	hi := uToBI(state.MaxSellDelta)
	if hi.Sign() <= 0 {
		return nil, nil, errTradeExceedsLimit
	}

	hiQuote, err := quoteSellExactIn(cloneQuoteState(state), hi)
	if err != nil {
		return nil, nil, err
	}
	if hiQuote.AmountOut.ToBig().Cmp(targetReservesOut) < 0 {
		return nil, nil, errTradeExceedsLimit
	}

	lo := big.NewInt(1)
	for new(big.Int).Sub(hi, lo).Cmp(big.NewInt(1)) > 0 {
		mid := divBI(addBI(lo, hi), twoBI)
		midQuote, quoteErr := quoteSellExactIn(cloneQuoteState(state), mid)
		if quoteErr == nil && midQuote.AmountOut.ToBig().Cmp(targetReservesOut) >= 0 {
			hi = mid
		} else {
			lo = mid
		}
	}

	finalQuote, err := quoteSellExactIn(cloneQuoteState(state), hi)
	if err != nil {
		return nil, nil, err
	}
	return hi, finalQuote.Fee.ToBig(), nil
}

func assertBigEqual(t *testing.T, label string, expected, actual *big.Int) {
	t.Helper()

	if expected.Cmp(actual) != 0 {
		t.Fatalf("%s mismatch: solidity=%s go=%s diff=%s", label, expected, actual, new(big.Int).Sub(actual, expected))
	}
}

func TestBaselineDifferentialEnvBlockNumberParse(t *testing.T) {
	t.Setenv("BASELINE_RPC_URL", "http://127.0.0.1:8545")
	t.Setenv("BASELINE_RELAY_ADDRESS", "0x0000000000000000000000000000000000000001")
	t.Setenv("BASELINE_BTOKEN_ADDRESS", "0x0000000000000000000000000000000000000002")
	t.Setenv("BASELINE_RESERVE_ADDRESS", "0x0000000000000000000000000000000000000003")
	t.Setenv("BASELINE_BLOCK_NUMBER", strconv.FormatUint(12345, 10))

	env := loadBaselineDifferentialEnv(t)
	if env.blockNumber == nil || env.blockNumber.Uint64() != 12345 {
		t.Fatalf("unexpected block number: %v", env.blockNumber)
	}
}
