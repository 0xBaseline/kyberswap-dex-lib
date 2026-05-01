package baseline

import (
	"errors"
	"math"
	"math/big"

	"github.com/holiman/uint256"
)

var (
	wadBI       = big.NewInt(1e18)
	twoBI       = big.NewInt(2)
	maxPowArgBI = new(big.Int).Mul(big.NewInt(135), wadBI)

	errTradeExceedsLimit = errors.New("trade exceeds limit")
	errPriceMustChange   = errors.New("price must change")
	errInvalidCurveState = errors.New("invalid curve state")
	errSolverFailed      = errors.New("solver failed")
)

func uToBI(x *uint256.Int) *big.Int {
	if x == nil {
		return new(big.Int)
	}
	return x.ToBig()
}

func biToU(x *big.Int) *uint256.Int {
	if x == nil || x.Sign() <= 0 {
		return uint256.NewInt(0)
	}
	return uint256.MustFromBig(x)
}

func addBI(x, y *big.Int) *big.Int {
	return new(big.Int).Add(x, y)
}

func subBI(x, y *big.Int) *big.Int {
	return new(big.Int).Sub(x, y)
}

func mulBI(x, y *big.Int) *big.Int {
	return new(big.Int).Mul(x, y)
}

func divBI(x, y *big.Int) *big.Int {
	if y.Sign() == 0 {
		return nil
	}
	return new(big.Int).Div(x, y)
}

func ceilDivBI(x, y *big.Int) *big.Int {
	if y.Sign() == 0 {
		return nil
	}
	q, r := new(big.Int).QuoRem(x, y, new(big.Int))
	if r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

func zeroFloorSubBI(x, y *big.Int) *big.Int {
	if x.Cmp(y) <= 0 {
		return new(big.Int)
	}
	return subBI(x, y)
}

func absBI(x *big.Int) *big.Int {
	return new(big.Int).Abs(x)
}

func mulWad(x, y *big.Int) *big.Int {
	return divBI(mulBI(x, y), wadBI)
}

func mulWadUp(x, y *big.Int) *big.Int {
	return ceilDivBI(mulBI(x, y), wadBI)
}

func divWad(x, y *big.Int) *big.Int {
	return divBI(mulBI(x, wadBI), y)
}

func divWadUp(x, y *big.Int) *big.Int {
	return ceilDivBI(mulBI(x, wadBI), y)
}

func fullMulDiv(x, y, d *big.Int) *big.Int {
	return divBI(mulBI(x, y), d)
}

func fullMulDivUp(x, y, d *big.Int) *big.Int {
	return ceilDivBI(mulBI(x, y), d)
}

func normalizeWadBI(amount *big.Int, decimals uint8) *big.Int {
	if decimals < 18 {
		return new(big.Int).Mul(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-decimals)), nil))
	}
	if decimals > 18 {
		return new(big.Int).Div(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-18)), nil))
	}
	return new(big.Int).Set(amount)
}

func denormalizeWadBI(amount *big.Int, decimals uint8) *big.Int {
	if decimals < 18 {
		return new(big.Int).Div(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-decimals)), nil))
	}
	if decimals > 18 {
		return new(big.Int).Mul(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-18)), nil))
	}
	return new(big.Int).Set(amount)
}

func denormalizeWadUpBI(amount *big.Int, decimals uint8) *big.Int {
	if decimals < 18 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-decimals)), nil)
		return ceilDivBI(amount, divisor)
	}
	if decimals > 18 {
		return new(big.Int).Mul(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-18)), nil))
	}
	return new(big.Int).Set(amount)
}

func toWadSignedBI(amount *big.Int, decimals uint8) *big.Int {
	if amount.Sign() >= 0 {
		return normalizeWadBI(amount, decimals)
	}
	return new(big.Int).Neg(normalizeWadBI(absBI(amount), decimals))
}

func powWadBI(x, y *big.Int, roundUp bool) (*big.Int, error) {
	if x.Sign() <= 0 {
		return nil, errInvalidCurveState
	}

	xf, _ := new(big.Float).SetPrec(512).SetInt(x).Float64()
	yf, _ := new(big.Float).SetPrec(512).SetInt(y).Float64()
	value := math.Pow(xf/1e18, yf/1e18) * 1e18
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, errTradeExceedsLimit
	}
	if roundUp {
		value = math.Ceil(value)
	} else {
		value = math.Floor(value)
	}
	res, ok := new(big.Int).SetString(new(big.Float).SetPrec(512).SetFloat64(value).Text('f', 0), 10)
	if !ok {
		return nil, errTradeExceedsLimit
	}
	if res.Sign() == 0 {
		return nil, errTradeExceedsLimit
	}
	return res, nil
}

func lnWadBI(x *big.Int) (*big.Int, error) {
	if x.Sign() <= 0 {
		return nil, errInvalidCurveState
	}
	xf, _ := new(big.Float).SetPrec(512).SetInt(x).Float64()
	value := math.Log(xf/1e18) * 1e18
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, errTradeExceedsLimit
	}
	res, ok := new(big.Int).SetString(new(big.Float).SetPrec(512).SetFloat64(math.Floor(value)).Text('f', 0), 10)
	if !ok {
		return nil, errTradeExceedsLimit
	}
	return res, nil
}

func checkPowLimit(ratio, convexityExp *big.Int) error {
	if ratio.Cmp(wadBI) == 0 {
		return nil
	}
	lnRatio, err := lnWadBI(ratio)
	if err != nil {
		return err
	}
	if mulWad(convexityExp, lnRatio).Cmp(maxPowArgBI) > 0 {
		return errTradeExceedsLimit
	}
	return nil
}
