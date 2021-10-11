//go:build peersrpc
// +build peersrpc

package peersrpc

type Config struct {
	// PeersMacPath is the path for the peers macaroon. If
	// unspecified then we assume that the macaroon will be found under the
	// network directory, named DefaultPeersMacFilename.
	PeersMacPath string `long:"peersmacaroonpath" description:"Path to the peers macaroon"`
}
