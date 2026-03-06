package baseline

import "errors"

const (
	DexType    = "baseline"
	defaultGas = 300000

	methodReserve         = "reserve"
	methodTotalReserves   = "totalReserves"
	methodTotalBTokens    = "totalBTokens"
	methodTotalSupply     = "totalSupply"
	methodQuoteBuyExactIn  = "quoteBuyExactIn"
	methodQuoteSellExactIn = "quoteSellExactIn"
)

var (
	ErrInvalidToken    = errors.New("invalid token")
	ErrInvalidAmountIn = errors.New("invalid amount in")
	ErrInvalidAmountOut = errors.New("invalid amount out")
	ErrPoolNotFound    = errors.New("pool not found")
	ErrNoRate          = errors.New("no cached rate")
)
