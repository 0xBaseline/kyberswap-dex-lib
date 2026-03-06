package baseline

import "github.com/holiman/uint256"

type Metadata struct {
	Offset int `json:"offset"`
}

// Extra is serialized into entity.Pool.Extra
type Extra struct {
	RelayAddress string `json:"r"`
	// BuyRate: [amountIn, amountOut] for reserve -> bToken (1 unit of reserve)
	BuyRate [2]*uint256.Int `json:"b"`
	// SellRate: [amountIn, amountOut] for bToken -> reserve (1 unit of bToken)
	SellRate [2]*uint256.Int `json:"s"`
}

type SwapInfo struct {
	RelayAddress string `json:"relayAddress"`
	BToken       string `json:"bToken"`
	IsBuy        bool   `json:"isBuy"`
}
