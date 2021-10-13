package itest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/peersrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/stretchr/testify/require"
)

const (
	// ChannelOpenTimeout is the max time we will wait before a channel to
	// be considered opened.
	ChannelOpenTimeout = time.Second * 30
)

// assertNodeAnnouncement is a helper function to compere if two node updates
// contain the same information
func assertNodeAnnouncement(n1, n2 *lnrpc.NodeUpdate) error {
	if n1.Alias != n2.Alias {
		return fmt.Errorf("different alias: %s != %s",
			n1.Alias, n2.Alias)
	}
	return nil
}

// Helper function to assert NodeAnnouncementUpdateResponse expected values
func checkResponse(response *peersrpc.NodeAnnouncementUpdateResponse,
	expectedOps map[string]int) error {

	if len(response.Ops) != len(expectedOps) {
		return fmt.Errorf("unexpected number of Ops updating dave's "+
			"node announcement: %v", response)
	}

	ops := make(map[string]int, len(response.Ops))
	for _, op := range response.Ops {
		ops[op.Entity] = len(op.Actions)
	}

	for k, v := range expectedOps {
		if v != ops[k] {
			return fmt.Errorf("unexpected number of actions for "+
				"operation %s: got %d wanted %d", k, ops[k], v)
		}
	}

	return nil
}

// waitForNodeAnnUpdates waits some time until the right node update is received
// or it times out
func waitForNodeAnnUpdates(graphSub graphSubscription,
	nodePubKey string, expectedUpdate *lnrpc.NodeUpdate) error {

	for {
		select {
		case graphUpdate := <-graphSub.updateChan:
			for _, update := range graphUpdate.NodeUpdates {
				if update.IdentityKey == nodePubKey {
					return assertNodeAnnouncement(update,
						expectedUpdate)
				}
			}
		case err := <-graphSub.errChan:
			return fmt.Errorf("unable to recv graph update: %v", err)
		case <-time.After(defaultTimeout):
			return fmt.Errorf("did not receive node ann update")
		}
	}
}

// testUpdateNodeAnnouncement ensures that the RPC endpoint validates
// the requests correctly and that the new node announcement is brodcasted
// with the right information after updating our node
func testUpdateNodeAnnouncement(net *lntest.NetworkHarness, t *harnessTest) {
	ctxb := context.Background()

	// Launch notification clients for alice, such that we can
	// get notified when there are updates in the graph.
	aliceSub := subscribeGraphNotifications(ctxb, t, net.Alice)
	defer close(aliceSub.quit)

	// Use the default address *explicitly* to make asserts more
	// readable
	advertisedAddrs := []string{
		"127.0.0.1:5569",
	}

	var lndArgs []string
	for _, addr := range advertisedAddrs {
		lndArgs = append(lndArgs, "--externalip="+addr)
	}

	dave := net.NewNode(t.t, "Dave", lndArgs)
	defer shutdownAndAssert(net, t, dave)

	// Get dave default information so we can compare
	// it lately with the new brodcasted updates
	nodeInfoReq := &lnrpc.GetInfoRequest{}
	resp, err := dave.GetInfo(ctxb, nodeInfoReq)
	require.NoError(t.t, err, "unable to get dave's information")

	defaultDaveNodeAnn := &lnrpc.NodeUpdate{
		Alias: resp.Alias,
		Color: resp.Color,
		NodeAddresses: []*lnrpc.NodeAddress{
			{Addr: advertisedAddrs[0], Network: "tcp"},
		},
	}

	_ = defaultDaveNodeAnn

	//TODO delte var
	var ctxt context.Context
	var invalidNodeAnnReq *peersrpc.NodeAnnouncementUpdateRequest

	// We cannot differentiate between requests with Alias = "" and requests
	// that do not provide that field. If a user sets Alias = "" in the request
	// the field will simply be ignored. The request must fail because no
	// modifiers are applied
	invalidNodeAnnReq = &peersrpc.NodeAnnouncementUpdateRequest{
		Alias: "",
	}
	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	_, err = dave.UpdateNodeAnnouncement(ctxt, invalidNodeAnnReq)
	require.Error(t.t, err, "requests with empty alias should ignore that field")

	// Alias too long
	invalidNodeAnnReq = &peersrpc.NodeAnnouncementUpdateRequest{
		Alias: strings.Repeat("a", 50),
	}
	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	_, err = dave.UpdateNodeAnnouncement(ctxt, invalidNodeAnnReq)
	require.Error(t.t, err, "failed to validate an invalid alias for an update node announcement request")

	// Test NodeAnnouncement broadcasting

	// We must let Dave have an open channel before he can send a node
	// announcement, so we open a channel with Bob,
	net.ConnectNodes(t.t, net.Bob, dave)

	// We'll then go ahead and open a channel between Bob and Dave. This
	// ensures that Alice receives the node announcement from Bob as part of
	// the announcement broadcast.
	ctxt, _ = context.WithTimeout(ctxb, ChannelOpenTimeout)
	chanPoint := openChannelAndAssert(
		t, net, net.Bob, dave,
		lntest.OpenChannelParams{
			Amt: 1000000,
		},
	)

	// We'll then wait for Alice to receive dave's node announcement
	// with the default values.
	err = waitForNodeAnnUpdates(
		aliceSub, dave.PubKeyStr, defaultDaveNodeAnn,
	)
	require.NoError(t.t, err, "error waiting for node updates")

	newAlias := "new-alias"
	nodeAnnReq := &peersrpc.NodeAnnouncementUpdateRequest{
		Alias: newAlias,
	}

	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	response, err := dave.UpdateNodeAnnouncement(ctxt, nodeAnnReq)
	require.NoError(t.t, err, "unable to update dave's node announcement")

	expectedOps := map[string]int{
		"alias": 1,
	}
	err = checkResponse(response, expectedOps)
	require.NoError(t.t, err, "unexpected UpdateNodeAnnouncement response")

	// Close the channel between Bob and Dave.
	ctxt, _ = context.WithTimeout(ctxb, channelCloseTimeout)
	closeChannelAndAssert(t, net, net.Bob, chanPoint, false)
}
