package random

import (
	"encoding/binary"

	"github.com/chihaya/chihaya/bittorrent"
)

// DeriveEntropyFromRequest generates 2*64 bits of pseudo random state from an
// AnnounceRequest.
//
// Calling DeriveEntropyFromRequest multiple times yields the same values.
func DeriveEntropyFromRequest(req *bittorrent.AnnounceRequest) (v0 uint64, v1 uint64) {
	if len(req.InfoHash) >= bittorrent.InfoHashV1Len {
		v0 = binary.BigEndian.Uint64([]byte(req.InfoHash[:8])) + binary.BigEndian.Uint64([]byte(req.InfoHash[8:16]))
	}
	v1 = binary.BigEndian.Uint64(req.Peer.ID[:8]) + binary.BigEndian.Uint64(req.Peer.ID[8:16])
	return
}
