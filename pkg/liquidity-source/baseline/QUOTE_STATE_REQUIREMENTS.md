# Baseline AMM Off-Chain Quote State Requirements

## Goal

KyberSwap quotes AMMs by running local Go simulators against cached pool state. For Baseline/Mercury, sampled buy/sell rates are not enough because Mercury is a nonlinear power-law AMM with block-batched dynamic pricing.

The contracts should expose one stable read-only quote-state API that gives off-chain indexers the exact quote context needed to reproduce:

- `BSwap.quoteBuyExactIn`
- `BSwap.quoteBuyExactOut`
- `BSwap.quoteSellExactIn`
- `BSwap.quoteSellExactOut`

This does not move quote search on-chain. Kyber will still do local Go simulation. The lens only provides the starting state for that simulation.

## Required Contract Addition

Add one `view` function on `BLens` or another read-only component:

```solidity
function getQuoteState(BToken bToken)
    external
    view
    returns (QuoteState memory state);
```

This function should not mutate state, advance block pricing, distribute fees, or settle anything.

## Required State

The contract should return the effective quote context, not every internal storage field used to derive it.

```solidity
struct QuoteState {
    // Exact curve snapshot used by MakerLib.quoteSwap / BSwap quote paths.
    // All CurveParams fields use the same WAD-normalized units as CurveLib.
    CurveParams snapshotCurveParams;

    // Effective same-block accumulators for this quote context.
    // These are native bToken units. bToken decimals are always 18.
    uint256 quoteBlockBuyDeltaCirc;
    uint256 quoteBlockSellDeltaCirc;

    // Native pool accounting used for solver bounds and local state updates.
    uint256 totalSupply;
    uint256 totalBTokens;
    uint256 totalReserves;

    // Reserve token decimals. bToken decimals are always 18 and do not need
    // to be returned.
    uint8 reserveDecimals;

    // Convenience fields from the same effective quote context.
    uint256 maxSellDelta;
    uint256 snapshotActivePrice;
}
```

Where `CurveParams` is the existing struct:

```solidity
struct CurveParams {
    uint256 BLV;
    uint256 circ;
    uint256 supply;
    uint256 swapFee;
    uint256 reserves;
    uint256 totalSupply;
    uint256 convexityExp;
    uint256 lastInvariant;
}
```

## Field Semantics

`snapshotCurveParams` must match `MakerLib.getSnapshotCurveParams(bToken)`. This is the most important field. It bakes in Mercury's block-pricing preview, deferred maker preview, pending surplus handling, and safety surplus handling.

`quoteBlockBuyDeltaCirc` and `quoteBlockSellDeltaCirc` must match the accumulators that `MakerLib.quoteSwap` would use for this quote context:

- If `State.blockPricing(bToken).blockNumber == block.number`, return the current stored block buy/sell accumulators.
- If `State.blockPricing(bToken).blockNumber != block.number`, return zero for both. In this case stale pending flow is already reflected in the previewed `snapshotCurveParams`; the old block's raw accumulators must not be replayed off-chain.

`totalSupply`, `totalBTokens`, and `totalReserves` are native token accounting fields used by Kyber's local simulator for solver bounds and route-local state updates.

`reserveDecimals` is needed to convert reserve amounts to and from WAD. The bToken side can assume 18 decimals.

`maxSellDelta` is a convenience field equivalent to `MakerLib.maxSellDelta(bToken)`. Go could recompute it, but exposing the contract-derived value avoids ambiguity around sell bounds.

`snapshotActivePrice` is a convenience field equivalent to:

```solidity
CurveLib.computeActivePrice(snapshotCurveParams)
```

Go could recompute it, but exposing it reduces duplicated math in solver estimates and gives parity tests a cheap sanity check.

## Why This Is Minimal

Kyber does not need the following for quote parity:

- current curve params
- raw maker benchmark fields
- raw stale block-pricing fields
- `startReserves`, `startSupply`, or `startLastInvariant`
- `pendingSurplus`
- `settledReserves`
- claimables, pending yield, or fee recipient fields
- initialized or paused flags, assuming pool discovery/tracking already filters invalid pools
- bToken decimals, because bTokens are always 18 decimals

Returning fewer fields reduces ABI coupling and avoids confusing raw storage values with the effective quote context.

## Contract-Side Sketch

```solidity
function getQuoteState(BToken bToken)
    external
    view
    returns (QuoteState memory s)
{
    State.Pool storage pool = State.pool(bToken);
    State.BlockPricing storage pricing = State.blockPricing(bToken);

    s.snapshotCurveParams = MakerLib.getSnapshotCurveParams(bToken);

    if (pricing.blockNumber == uint64(block.number)) {
        s.quoteBlockBuyDeltaCirc = pricing.blockBuyDeltaCirc;
        s.quoteBlockSellDeltaCirc = pricing.blockSellDeltaCirc;
    }

    s.totalSupply = pool.totalSupply;
    s.totalBTokens = pool.totalBTokens;
    s.totalReserves = pool.totalReserves;
    s.reserveDecimals = pool.reserveDecimals;
    s.maxSellDelta = MakerLib.maxSellDelta(bToken);
    s.snapshotActivePrice = CurveLib.computeActivePrice(s.snapshotCurveParams);
}
```

## Kyber-Side Usage

Kyber's `PoolTracker` will call `getQuoteState` once per pool refresh and serialize the returned state into `entity.Pool.Extra`.

Kyber's `PoolSimulator` will locally implement:

- `quoteBuyExactIn`
- `quoteBuyExactOut`
- `quoteSellExactIn`
- `quoteSellExactOut`
- `BlockPricingLib.quoteSwap`
- `CurveLib.computeSwap`
- Solady-compatible fixed-point math

During route simulation, `UpdateBalance` will apply the local state transition and update the effective same-block accumulators, similar to other stateful AMM simulators.

## Testing Expectations

After adding `getQuoteState`, build differential tests that:

1. Fetch `getQuoteState`.
2. Run the Go quote locally.
3. Compare against on-chain view calls:
   - `quoteBuyExactIn`
   - `quoteBuyExactOut`
   - `quoteSellExactIn`
   - `quoteSellExactOut`
4. Test multiple trade sizes:
   - tiny trade
   - 1 unit
   - medium trade
   - large trade near solver bounds
5. Test normal and non-18-decimal reserve tokens if applicable.
6. Test after at least one in-block buy/sell on a fork or local deployment to verify accumulator behavior.

## Recommendation

Expose only the minimal `QuoteState` above. It gives Kyber the effective quote context without tying the integration to Mercury's raw internal storage layout.
