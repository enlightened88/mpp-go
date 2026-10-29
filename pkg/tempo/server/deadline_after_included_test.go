package chargeserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tempoxyz/mpp-go/pkg/tempo"
	temporpc "github.com/tempoxyz/tempo-go/pkg/client"
)

// A sponsored retry made when less than the minimum fee payer window remains
// before ValidBefore must still recover the receipt of an included transaction.
func TestChargeFlow_FeePayerRetryRecoversAfterValidityWindowShrinks(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps ~47s")
	}
	ctx := context.Background()
	request := buildRequest(t, true, nil)
	rpc := newMockRPC(request)
	failReceiptFetch := true
	rpc.onGetReceipt = func(hash string) (*temporpc.JSONRPCResponse, error) {
		if failReceiptFetch && len(rpc.sentRawTxs) > 0 {
			failReceiptFetch = false
			return nil, fmt.Errorf("temporary receipt rpc failure")
		}
		return &temporpc.JSONRPCResponse{Result: rpc.receipts[hash]}, nil
	}
	credential, err := newClientMethod(t, rpc, tempo.CredentialTypeTransaction).CreateCredential(ctx, buildChallenge(t, request))
	require.NoError(t, err)
	intent, err := NewIntent(IntentConfig{Store: tempo.NewMemoryStore(), RPC: rpc, FeePayerPrivateKey: feePayerKey})
	require.NoError(t, err)

	_, err = intent.Verify(ctx, credential, request.Map())
	require.ErrorContains(t, err, "outcome is unknown")
	require.Len(t, rpc.sentRawTxs, 1)

	// ValidBefore is signing time + 60s; wait until under 15s remain.
	time.Sleep(47 * time.Second)

	rpc.onCall = drainedBalanceCall
	receipt, err := intent.Verify(ctx, credential, request.Map())
	require.NoError(t, err, "included tx must be recoverable even near ValidBefore")
	require.Len(t, rpc.sentRawTxs, 1)
	assert.Equal(t, mustTxHash(rpc.sentRawTxs[0]), receipt.Reference)
}
