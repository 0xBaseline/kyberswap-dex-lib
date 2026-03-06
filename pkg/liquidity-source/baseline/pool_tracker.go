package baseline

import (
	"context"
	"math/big"
	"time"

	"github.com/KyberNetwork/ethrpc"
	"github.com/KyberNetwork/logger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"
	"github.com/holiman/uint256"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	pooltrack "github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool/tracker"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/bignumber"
)

type PoolTracker struct {
	config       *Config
	ethrpcClient *ethrpc.Client
}

var _ = pooltrack.RegisterFactoryCE(DexType, NewPoolTracker)

func NewPoolTracker(
	cfg *Config,
	ethrpcClient *ethrpc.Client,
) (*PoolTracker, error) {
	return &PoolTracker{
		config:       cfg,
		ethrpcClient: ethrpcClient,
	}, nil
}

func (d *PoolTracker) GetNewPoolState(
	ctx context.Context,
	p entity.Pool,
	_ pool.GetNewPoolStateParams,
) (entity.Pool, error) {
	logger.Infof("[Baseline] Start getting new state of pool: %v", p.Address)

	if len(p.Tokens) != 2 {
		return entity.Pool{}, ErrPoolNotFound
	}

	// Pool address is the bToken address
	bTokenAddr := common.HexToAddress(p.Address)

	// Token[0] = reserve, Token[1] = bToken (as set in pools_list_updater)
	reserveDecimals := uint8(18)
	bTokenDecimals := uint8(18)
	if p.Tokens[0].Decimals > 0 {
		reserveDecimals = p.Tokens[0].Decimals
	}
	if p.Tokens[1].Decimals > 0 {
		bTokenDecimals = p.Tokens[1].Decimals
	}

	buyAmountIn := bignumber.TenPowInt(reserveDecimals)
	sellAmountIn := bignumber.TenPowInt(bTokenDecimals)

	var (
		totalReserves *big.Int
		totalBTokens  *big.Int
		buyQuote      struct{ TokensOut, FeesReceived, Slippage *big.Int }
		sellQuote     struct{ AmountOut, FeesReceived, Slippage *big.Int }
	)

	req := d.ethrpcClient.NewRequest().SetContext(ctx)
	req.AddCall(&ethrpc.Call{
		ABI:    relayABI,
		Target: d.config.RelayAddress,
		Method: methodTotalReserves,
		Params: []any{bTokenAddr},
	}, []any{&totalReserves})
	req.AddCall(&ethrpc.Call{
		ABI:    relayABI,
		Target: d.config.RelayAddress,
		Method: methodTotalBTokens,
		Params: []any{bTokenAddr},
	}, []any{&totalBTokens})
	req.AddCall(&ethrpc.Call{
		ABI:    relayABI,
		Target: d.config.RelayAddress,
		Method: methodQuoteBuyExactIn,
		Params: []any{bTokenAddr, buyAmountIn},
	}, []any{&buyQuote})
	req.AddCall(&ethrpc.Call{
		ABI:    relayABI,
		Target: d.config.RelayAddress,
		Method: methodQuoteSellExactIn,
		Params: []any{bTokenAddr, sellAmountIn},
	}, []any{&sellQuote})

	if _, err := req.TryAggregate(); err != nil {
		logger.WithFields(logger.Fields{
			"poolAddress": p.Address,
			"error":       err,
		}).Errorf("[Baseline] failed to fetch pool state")
		return entity.Pool{}, err
	}

	extra := Extra{
		RelayAddress: d.config.RelayAddress,
	}

	// Buy rate: reserve -> bToken
	if buyQuote.TokensOut != nil && buyQuote.TokensOut.Sign() > 0 {
		extra.BuyRate = [2]*uint256.Int{
			uint256.MustFromBig(buyAmountIn),
			uint256.MustFromBig(buyQuote.TokensOut),
		}
	}

	// Sell rate: bToken -> reserve
	if sellQuote.AmountOut != nil && sellQuote.AmountOut.Sign() > 0 {
		extra.SellRate = [2]*uint256.Int{
			uint256.MustFromBig(sellAmountIn),
			uint256.MustFromBig(sellQuote.AmountOut),
		}
	}

	extraBytes, err := json.Marshal(extra)
	if err != nil {
		return entity.Pool{}, err
	}

	reserveStr := "0"
	bTokenStr := "0"
	if totalReserves != nil {
		reserveStr = totalReserves.String()
	}
	if totalBTokens != nil {
		bTokenStr = totalBTokens.String()
	}

	p.Reserves = entity.PoolReserves{reserveStr, bTokenStr}
	p.Extra = string(extraBytes)
	p.Timestamp = time.Now().Unix()

	logger.Infof("[Baseline] Finish getting new state of pool: %v", p.Address)
	return p, nil
}
