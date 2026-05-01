package baseline

import (
	"math/big"
	"slices"
	"strings"

	"github.com/goccy/go-json"
	"github.com/samber/lo"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
)

type PoolSimulator struct {
	pool.Pool
	extra Extra
}

var _ = pool.RegisterFactory0(DexType, NewPoolSimulator)

func NewPoolSimulator(entityPool entity.Pool) (*PoolSimulator, error) {
	var extra Extra
	if len(entityPool.Extra) > 0 {
		if err := json.Unmarshal([]byte(entityPool.Extra), &extra); err != nil {
			return nil, err
		}
	}

	reserves := make([]*big.Int, len(entityPool.Tokens))
	for i, r := range entityPool.Reserves {
		reserve, ok := new(big.Int).SetString(r, 10)
		if !ok {
			reserve = big.NewInt(0)
		}
		reserves[i] = reserve
	}

	info := pool.PoolInfo{
		Address:  strings.ToLower(entityPool.Address),
		Exchange: entityPool.Exchange,
		Type:     entityPool.Type,
		Tokens:   lo.Map(entityPool.Tokens, func(e *entity.PoolToken, _ int) string { return e.Address }),
		Reserves: reserves,
	}

	return &PoolSimulator{
		Pool:  pool.Pool{Info: info},
		extra: extra,
	}, nil
}

func (p *PoolSimulator) CalcAmountOut(param pool.CalcAmountOutParams) (*pool.CalcAmountOutResult, error) {
	tokenAmountIn, tokenOut := param.TokenAmountIn, param.TokenOut

	tokenInIndex := p.GetTokenIndex(tokenAmountIn.Token)
	tokenOutIndex := p.GetTokenIndex(tokenOut)
	if tokenInIndex < 0 || tokenOutIndex < 0 {
		return nil, ErrInvalidToken
	}
	if tokenAmountIn.Amount == nil || tokenAmountIn.Amount.Sign() <= 0 {
		return nil, ErrInvalidAmountIn
	}

	// Token[0] = reserve, Token[1] = bToken
	// reserve -> bToken = buy (tokenInIndex=0, tokenOutIndex=1)
	// bToken -> reserve = sell (tokenInIndex=1, tokenOutIndex=0)
	isBuy := tokenInIndex == 0

	quote, err := p.quoteAmountOut(isBuy, tokenAmountIn.Amount)
	if err != nil {
		return nil, err
	}
	amountOut := quote.AmountOut.ToBig()
	if amountOut.Sign() <= 0 {
		return nil, ErrNoRate
	}
	if amountOut.Cmp(p.GetReserves()[tokenOutIndex]) > 0 {
		return nil, ErrInvalidAmountOut
	}

	swapInfo := SwapInfo{
		RelayAddress: p.extra.RelayAddress,
		BToken:       p.Info.Address,
		IsBuy:        isBuy,
		State:        quote.State,
	}
	if isBuy {
		swapInfo.AmountOut = amountOut.String()
	}
	if quote.ReserveDelta != nil {
		swapInfo.ReserveDelta = quote.ReserveDelta.String()
	}
	if quote.Fee != nil {
		swapInfo.Fee = quote.Fee.String()
	}

	return &pool.CalcAmountOutResult{
		TokenAmountOut: &pool.TokenAmount{Token: tokenOut, Amount: amountOut},
		Fee:            &pool.TokenAmount{Token: p.Info.Tokens[0], Amount: quote.Fee.ToBig()},
		SwapInfo:       swapInfo,
		Gas:            defaultGas,
	}, nil
}

func (p *PoolSimulator) CloneState() pool.IPoolSimulator {
	cloned := *p
	cloned.Info.Reserves = slices.Clone(p.Info.Reserves)
	cloned.extra.QuoteState = cloneQuoteState(p.extra.QuoteState)
	return &cloned
}

func (p *PoolSimulator) UpdateBalance(params pool.UpdateBalanceParams) {
	tokenAmtIn, tokenAmtOut := params.TokenAmountIn, params.TokenAmountOut
	inIndex := p.GetTokenIndex(tokenAmtIn.Token)
	outIndex := p.GetTokenIndex(tokenAmtOut.Token)
	p.Info.Reserves = slices.Clone(p.Info.Reserves)
	p.Info.Reserves[inIndex] = new(big.Int).Add(p.Info.Reserves[inIndex], tokenAmtIn.Amount)
	p.Info.Reserves[outIndex] = new(big.Int).Sub(p.Info.Reserves[outIndex], tokenAmtOut.Amount)

	if swapInfo, ok := params.SwapInfo.(SwapInfo); ok && swapInfo.State != nil {
		p.extra.QuoteState = cloneQuoteState(swapInfo.State)
		p.Info.Reserves[0] = uToBI(swapInfo.State.TotalReserves)
		p.Info.Reserves[1] = uToBI(swapInfo.State.TotalBTokens)
	}
}

func (p *PoolSimulator) GetMetaInfo(_, _ string) any {
	return nil
}
