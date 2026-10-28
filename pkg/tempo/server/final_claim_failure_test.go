package chargeserver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tempoxyz/mpp-go/pkg/tempo"
)

// failNthPutIfAbsentStore fails the failOn-th PutIfAbsent call once.
type failNthPutIfAbsentStore struct {
	tempo.Store
	failOn int
	calls  int
	keys   []string
}

func (s *failNthPutIfAbsentStore) PutIfAbsent(ctx context.Context, key, value string) (bool, error) {
	s.calls++
	s.keys = append(s.keys, key)
	if s.calls == s.failOn {
		return false, errors.New("store unavailable")
	}
	return s.Store.PutIfAbsent(ctx, key, value)
}

// A sponsored payment whose final replay claim fails after the transaction is
// included must be recoverable by retry, as the broadcastTransaction comment says.
func TestChargeFlow_FeePayerRetryAfterFinalClaimStoreFailure(t *testing.T) {
	ctx := context.Background()
	request := buildRequest(t, true, nil)
	rpc := newMockRPC(request)
	credential, err := newClientMethod(t, rpc, tempo.CredentialTypeTransaction).CreateCredential(ctx, buildChallenge(t, request))
	require.NoError(t, err)

	// Call 1 is the sponsored claim, call 2 is the final ChargeStoreKey(txHash) claim.
	store := &failNthPutIfAbsentStore{Store: tempo.NewMemoryStore(), failOn: 2}
	intent, err := NewIntent(IntentConfig{Store: store, RPC: rpc, FeePayerPrivateKey: feePayerKey})
	require.NoError(t, err)

	_, err = intent.Verify(ctx, credential, request.Map())
	require.ErrorContains(t, err, "store unavailable")
	require.Len(t, rpc.sentRawTxs, 1)
	t.Logf("PutIfAbsent keys on first attempt: %v", store.keys)

	// The transaction is on chain and the payer's balance is spent.
	rpc.onCall = drainedBalanceCall
	receipt, err := intent.Verify(ctx, credential, request.Map())
	require.NoError(t, err, "retry after a store failure must recover the receipt")
	require.Len(t, rpc.sentRawTxs, 1)
	assert.Equal(t, mustTxHash(rpc.sentRawTxs[0]), receipt.Reference)
}
