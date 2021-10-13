//go:build peersrpc
// +build peersrpc

package peersrpc

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/netann"
	"google.golang.org/grpc"
	"gopkg.in/macaroon-bakery.v2/bakery"
)

const (
	// subServerName is the name of the sub rpc server. We'll use this name
	// to register ourselves, and we also require that the main
	// SubServerConfigDispatcher instance recognize tt as the name of our
	// RPC service.
	subServerName = "PeersRPC"
)

var (
	// macaroonOps are the set of capabilities that our minted macaroon (if
	// it doesn't already exist) will have.
	macaroonOps = []bakery.Op{}

	// macPermissions maps RPC calls to the permissions they require.
	macPermissions = map[string][]bakery.Op{}

	// DefaultPeersMacFilename is the default name of the peers macaroon
	// that we expect to find via a file handle within the main
	// configuration file in this package.
	DefaultPeersMacFilename = "peers.macaroon"
)

// ServerShell is a shell struct holding a reference to the actual sub-server.
// It is used to register the gRPC sub-server with the root server before we
// have the necessary dependencies to populate the actual sub-server.
type ServerShell struct {
	PeersServer
}

// Server is a sub-server of the main RPC server: the peers RPC. This sub
// RPC server allows to intereact with our Peers in the Lightning Network.
type Server struct {
	started  int32 // To be used atomically.
	shutdown int32 // To be used atomically.

	// Required by the grpc-gateway/v2 library for forward compatibility.
	// Must be after the atomically used variables to not break struct
	// alignment.
	UnimplementedPeersServer

	cfg *Config
}

// A compile time check to ensure that Server fully implements the PeersServer
// gRPC service.
var _ PeersServer = (*Server)(nil)

// New returns a new instance of the peersrpc Peers sub-server. We also
// return the set of permissions for the macaroons that we may create within
// this method. If the macaroons we need aren't found in the filepath, then
// we'll create them on start up. If we're unable to locate, or create the
// macaroons we need, then we'll return with an error.
func New(cfg *Config) (*Server, lnrpc.MacaroonPerms, error) {
	// If the path of the signer macaroon wasn't generated, then we'll
	// assume that it's found at the default network directory.
	if cfg.PeersMacPath == "" {
		cfg.PeersMacPath = filepath.Join(
			cfg.NetworkDir, DefaultPeersMacFilename,
		)
	}

	// Now that we know the full path of the peers macaroon, we can check
	// to see if we need to create it or not. If stateless_init is set
	// then we don't write the macaroons.
	macFilePath := cfg.PeersMacPath
	if cfg.MacService != nil && !cfg.MacService.StatelessInit &&
		!lnrpc.FileExists(macFilePath) {

		log.Infof("Making macaroons for Peers RPC Server at: %v",
			macFilePath)

		// At this point, we know that the signer macaroon doesn't yet,
		// exist, so we need to create it with the help of the main
		// macaroon service.
		peersMac, err := cfg.MacService.NewMacaroon(
			context.Background(), macaroons.DefaultRootKeyID,
			macaroonOps...,
		)
		if err != nil {
			return nil, nil, err
		}
		peersMacBytes, err := peersMac.M().MarshalBinary()
		if err != nil {
			return nil, nil, err
		}
		err = ioutil.WriteFile(macFilePath, peersMacBytes, 0644)
		if err != nil {
			_ = os.Remove(macFilePath)
			return nil, nil, err
		}
	}

	// We don't create any new macaroons for this subserver, instead reuse
	// existing onchain/offchain permissions.
	server := &Server{
		cfg: cfg,
	}

	return server, macPermissions, nil
}

// Start launches any helper goroutines required for the Server to function.
//
// NOTE: This is part of the lnrpc.SubServer interface.
func (s *Server) Start() error {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return nil
	}

	return nil
}

// Stop signals any active goroutines for a graceful closure.
//
// NOTE: This is part of the lnrpc.SubServer interface.
func (s *Server) Stop() error {
	if atomic.AddInt32(&s.shutdown, 1) != 1 {
		return nil
	}

	return nil
}

// Name returns a unique string representation of the sub-server. This can be
// used to identify the sub-server and also de-duplicate them.
//
// NOTE: This is part of the lnrpc.SubServer interface.
func (s *Server) Name() string {
	return subServerName
}

// RegisterWithRootServer will be called by the root gRPC server to direct a
// sub RPC server to register itself with the main gRPC root server. Until this
// is called, each sub-server won't be able to have
// requests routed towards it.
//
// NOTE: This is part of the lnrpc.GrpcHandler interface.
func (r *ServerShell) RegisterWithRootServer(grpcServer *grpc.Server) error {
	// We make sure that we register it with the main gRPC server to ensure
	// all our methods are routed properly.
	RegisterPeersServer(grpcServer, r)

	log.Debugf("Peers RPC server successfully register with root " +
		"gRPC server")

	return nil
}

// RegisterWithRestServer will be called by the root REST mux to direct a sub
// RPC server to register itself with the main REST mux server. Until this is
// called, each sub-server won't be able to have requests routed towards it.
//
// NOTE: This is part of the lnrpc.GrpcHandler interface.
func (r *ServerShell) RegisterWithRestServer(ctx context.Context,
	mux *runtime.ServeMux, dest string, opts []grpc.DialOption) error {

	return nil
}

// CreateSubServer populates the subserver's dependencies using the passed
// SubServerConfigDispatcher. This method should fully initialize the
// sub-server instance, making it ready for action. It returns the macaroon
// permissions that the sub-server wishes to pass on to the root server for all
// methods routed towards it.
//
// NOTE: This is part of the lnrpc.GrpcHandler interface.
func (r *ServerShell) CreateSubServer(configRegistry lnrpc.SubServerConfigDispatcher) (
	lnrpc.SubServer, lnrpc.MacaroonPerms, error) {

	subServer, macPermissions, err := createNewSubServer(configRegistry)
	if err != nil {
		return nil, nil, err
	}

	r.PeersServer = subServer
	return subServer, macPermissions, nil
}

// UpdateNodeAnnouncement allows the caller to update the node parameters
// and broadcasts a new version of the node announcement to its peers.
func (r *Server) UpdateNodeAnnouncement(_ context.Context,
	req *NodeAnnouncementUpdateRequest) (
	*NodeAnnouncementUpdateResponse, error) {

	resp := &NodeAnnouncementUpdateResponse{}
	nodeModifiers := make([]netann.NodeAnnModifier, 0)

	currentNodeAnn, err := r.cfg.GenNodeAnnouncement(false)
	if err != nil {
		return nil, fmt.Errorf("unable to get current node "+
			"announcement: %v", err)
	}

	if req.Alias != "" {
		alias, err := lnwire.NewNodeAlias(req.Alias)
		if err != nil {
			return nil, fmt.Errorf("invalid alias value: %v", err)
		}
		if alias != currentNodeAnn.Alias {
			resp.Ops = append(resp.Ops, &lnrpc.Op{
				Entity: "alias",
				Actions: []string{
					fmt.Sprintf("changed to %v", alias),
				},
			})
			nodeModifiers = append(nodeModifiers,
				netann.NodeAnnSetAlias(alias))
		}
	}

	if len(nodeModifiers) == 0 {
		return nil, fmt.Errorf("unable detect any new values to " +
			"update the node announcement")
	}

	// Then, we'll generate a new timestamped node
	// announcement with the updated addresses and broadcast
	// it to our peers.
	newNodeAnn, err := r.cfg.GenNodeAnnouncement(
		true, nodeModifiers...,
	)
	if err != nil {
		log.Debugf("Unable to generate new node "+
			"node announcement: %v", err)
		return nil, err
	}

	// Update the on-disk version of our announcement so
	// other sub-systems will become out of sync
	selfNode := &channeldb.LightningNode{
		HaveNodeAnnouncement: true,
		LastUpdate:           time.Unix(int64(newNodeAnn.Timestamp), 0),
		Addresses:            newNodeAnn.Addresses,
		Alias:                newNodeAnn.Alias.String(),
		Features: lnwire.NewFeatureVector(
			newNodeAnn.Features, lnwire.Features,
		),
		Color:        newNodeAnn.RGBColor,
		AuthSigBytes: newNodeAnn.Signature.ToSignatureBytes(),
	}
	copy(selfNode.PubKeyBytes[:], r.cfg.PubKey)

	if err := r.cfg.GraphDB.SetSourceNode(selfNode); err != nil {
		return nil, fmt.Errorf("can't set self node: %v", err)
	}

	// Finally, propagate it to the nodes in the network.
	err = r.cfg.Broadcast(nil, &newNodeAnn)
	if err != nil {
		log.Debugf("Unable to broadcast new node "+
			"announcement to peers: %v", err)
		return nil, err
	}

	return resp, nil
}
