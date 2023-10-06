package abci

import (
	"fmt"

	"cosmossdk.io/log"
	"encoding/json"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

// ScamProposalTx defines the custom transaction identifying the scam proposal by its ID.
type ScamProposalTx struct {
	ProposalID uint64
	IsScam     bool
}

// NewProposalHandler creates a new instance of the handler to be used.
func NewProposalHandler(
	lg log.Logger,
	valStore baseapp.ValidatorStore,
	cdc codec.Codec,
	govKeeper govkeeper.Keeper,
	stakingKeeper *stakingkeeper.Keeper,
) *ProposalHandler {
	return &ProposalHandler{
		logger:        lg,
		valStore:      valStore,
		cdc:           cdc,
		govKeeper:     govKeeper,
		stakingKeeper: stakingKeeper,
	}
}

// PrepareProposalHandler is the handler to be used for PrepareProposal.
func (h *ProposalHandler) PrepareProposalHandler() sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		err := baseapp.ValidateVoteExtensions(ctx, h.valStore, req.Height, ctx.ChainID(), req.LocalLastCommit)
		if err != nil {
			return nil, err
		}

		//_ := req.Txs

		if req.Height >= ctx.ConsensusParams().Abci.VoteExtensionsEnableHeight {
			stakeWeightedPrices, err := h.computeStakeWeightedOraclePrices(ctx, req.LocalLastCommit)
			if err != nil {
				return nil, errors.New("failed to compute stake-weighted oracle prices")
			}

			injectedVoteExtTx := StakeWeightedPrices{
				StakeWeightedPrices: stakeWeightedPrices,
				ExtendedCommitInfo:  req.LocalLastCommit,
			}

			// NOTE: We use stdlib JSON encoding, but an application may choose to use
			// a performant mechanism. This is for demo purposes only.
			bz, err := json.Marshal(injectedVoteExtTx)
			if err != nil {
				h.logger.Error("failed to encode injected vote extension tx", "err", err)
				return nil, errors.New("failed to encode injected vote extension tx")
			}

			// Inject a "fake" tx into the proposal s.t. validators can decode, verify,
			// and store the canonical stake-weighted average prices.
			proposalTxs = append(proposalTxs, bz)
		}



		return nil, nil
	}
}

// ProcessProposalHandler is the handler to be used for ProcessProposal.
func (h *ProposalHandler) ProcessProposalHandler() sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (resp *abci.ResponseProcessProposal, err error) {
		resp.Status = 1 // Accepts the proposal
		return resp, nil
	}
}

func (h *ProposalHandler) PreBlocker(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	return &sdk.ResponsePreBlock{}, nil
}

// computeScamIdentificationResults aggregates the scam identification results from each validator.
func (h *ProposalHandler) computeScamIdentificationResults(ctx sdk.Context, ci abci.ExtendedCommitInfo) (int64, error) {
	// Get all the votes from the commit info
	var weightedScore uint64
	var totalStake int64
	for i, vote := range ci.Votes {
		if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit {
			continue
		}

		var scamPropExt ScamProposalExtension
		if err := json.Unmarshal(vote.VoteExtension, &scamPropExt); err != nil {
			h.logger.Error("failed to decode vote extension", "err", err, "validator", fmt.Sprintf("%x", vote.Validator.Address))
			// We used 101 because is outside our range of interested and will be ignored by the caller
			return -1, err
		}

		totalStake += vote.Validator.Power
		// Compute stake-weighted sum of the scamScore, i.e.
		// (S1)(W1) + (S2)(W2) + ... + (Sn)(Wn) / (W1 + W2 + ... + Wn)
		weightedScore += int64(scamPropExt.ScamPercent) * vote.Validator.Power
	}

	// Compute stake-weighted average of the scamScore, i.e.
	// (S1)(W1) + (S2)(W2) + ... + (Sn)(Wn) / (W1 + W2 + ... + Wn)
	return weightedScore / totalStake, nil

}
