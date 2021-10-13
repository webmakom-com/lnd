//go:build peersrpc
// +build peersrpc

package peersrpc

import (
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/netann"
	"github.com/lightningnetwork/lnd/routing/route"
)

type Config struct {
	// Broadcast broadcasts a particular set of announcements to all peers
	// that the daemon is connected to. If supplied, the exclude parameter
	// indicates that the target peer should be excluded from the
	// broadcast.
	//
	// NOTE: This function is safe for concurrent access.
	Broadcast func(skips map[route.Vertex]struct{},
		msg ...lnwire.Message) error

	// GenNodeAnnouncement is used to send our node announcement to the remote
	// on startup.
	GenNodeAnnouncement func(bool,
		...netann.NodeAnnModifier) (lnwire.NodeAnnouncement, error)

	// GraphDB is a global database instance which is needed to access the
	// channel graph.
	GraphDB *channeldb.ChannelGraph

	// NetworkDir is the main network directory wherein the peers rpc
	// server will find the macaroon named DefaultPeersMacFilename.
	NetworkDir string

	// MacService is the main macaroon service that we'll use to handle
	// authentication for the peers rpc server.
	MacService *macaroons.Service

	// PeersMacPath is the path for the peers macaroon. If
	// unspecified then we assume that the macaroon will be found under the
	// network directory, named DefaultPeersMacFilename.
	PeersMacPath string `long:"macaroonpath" description:"Path to the peers macaroon"`

	// PubKey is the node's public key.
	PubKey []byte
}
