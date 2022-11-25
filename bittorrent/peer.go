// Package bittorrent implements all of the abstractions used to decouple the
// protocol of a BitTorrent tracker from the logic of handling Announces and
// Scrapes.
package bittorrent

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/rs/zerolog"
)

// PeerIDLen is length of peer id field in bytes
const PeerIDLen = 20

// PeerID represents a peer ID.
type PeerID [PeerIDLen]byte

// ErrInvalidPeerIDSize holds error about invalid PeerID size
var ErrInvalidPeerIDSize = fmt.Errorf("peer ID must be %d bytes", PeerIDLen)

// NewPeerID creates a PeerID from a byte slice.
//
// It panics if b is not 20 bytes long.
func NewPeerID(b []byte) (PeerID, error) {
	var p PeerID
	if len(b) != PeerIDLen {
		return p, ErrInvalidPeerIDSize
	}
	copy(p[:], b)
	return p, nil
}

// String implements fmt.Stringer, returning the base16 encoded PeerID.
func (p PeerID) String() string {
	return hex.EncodeToString(p[:])
}

// RawString returns a 20-byte string of the raw bytes of the ID.
func (p PeerID) RawString() string {
	return string(p[:])
}

// InfoHash represents an infohash.
type InfoHash string

const (
	// InfoHashV1Len is the same as sha1.Size
	InfoHashV1Len = sha1.Size
	// InfoHashV2Len ... sha256.Size
	InfoHashV2Len = sha256.Size
	// NoneInfoHash dummy invalid InfoHash
	NoneInfoHash InfoHash = ""
)

var (
	// ErrInvalidHashType holds error about invalid InfoHash input type
	ErrInvalidHashType = errors.New("info hash must be provided as byte slice or raw/hex string")
	// ErrInvalidHashSize holds error about invalid InfoHash size
	ErrInvalidHashSize = fmt.Errorf("info hash must be either %d (for torrent V1) or %d (V2) bytes", InfoHashV1Len, InfoHashV2Len)
)

// TruncateV1 returns truncated to 20-bytes length array of the corresponding InfoHash.
// If InfoHash is V2 (32 bytes), it will be truncated to 20 bytes
// according to BEP52.
func (i InfoHash) TruncateV1() InfoHash {
	if len(i) == InfoHashV2Len {
		return i[:InfoHashV1Len]
	}
	return i
}

// NewInfoHash creates an InfoHash from a byte slice or raw/hex string.
func NewInfoHash(data any) (InfoHash, error) {
	if data == nil {
		return NoneInfoHash, ErrInvalidHashType
	}
	var ba []byte
	switch t := data.(type) {
	case [InfoHashV1Len]byte:
		ba = t[:]
	case [InfoHashV2Len]byte:
		ba = t[:]
	case []byte:
		ba = t
	case string:
		l := len(t)
		if l == InfoHashV1Len*2 || l == InfoHashV2Len*2 {
			var err error
			if ba, err = hex.DecodeString(t); err != nil {
				return NoneInfoHash, err
			}
		} else {
			ba = []byte(t)
		}
	}
	l := len(ba)
	if l != InfoHashV1Len && l != InfoHashV2Len {
		return NoneInfoHash, ErrInvalidHashSize
	}
	return InfoHash(ba), nil
}

// String implements fmt.Stringer, returning the base16 encoded InfoHash.
func (i InfoHash) String() string {
	return hex.EncodeToString([]byte(i))
}

// RawString returns a string of the raw bytes of the InfoHash.
func (i InfoHash) RawString() string {
	return string(i)
}

// Peer represents the connection details of a peer that is returned in an
// announce response.
type Peer struct {
	ID PeerID
	netip.AddrPort
}

// PeerMinimumLen is the least allowed length of string serialized Peer
const PeerMinimumLen = PeerIDLen + 2 + net.IPv4len

// ErrInvalidPeerDataSize holds error about invalid Peer data size
var ErrInvalidPeerDataSize = fmt.Errorf("invalid peer data it must be at least %d bytes (InfoHash + Port + IPv4)", PeerMinimumLen)

// NewPeer constructs Peer from serialized by Peer.RawString data: PeerID[20by]Port[2by]net.IP[4/16by]
func NewPeer(data string) (Peer, error) {
	var peer Peer
	if len(data) < PeerMinimumLen {
		return peer, ErrInvalidPeerDataSize
	}
	b := []byte(data)
	peerID, err := NewPeerID(b[:PeerIDLen])
	if err == nil {
		if addr, isOk := netip.AddrFromSlice(b[PeerIDLen+2:]); isOk {
			peer = Peer{
				ID: peerID,
				AddrPort: netip.AddrPortFrom(
					addr.Unmap(),
					binary.BigEndian.Uint16(b[PeerIDLen:PeerIDLen+2]),
				),
			}
		} else {
			err = ErrInvalidIP
		}
	}

	return peer, err
}

// RawString generates concatenation of PeerID, net port and IP-address
func (p Peer) RawString() string {
	ip := p.Addr()
	b := make([]byte, PeerIDLen+2+(ip.BitLen()/8))
	copy(b[:PeerIDLen], p.ID[:])
	binary.BigEndian.PutUint16(b[PeerIDLen:PeerIDLen+2], p.Port())
	copy(b[PeerIDLen+2:], ip.AsSlice())
	return string(b)
}

// Addr returns unmapped peer's IP address
func (p Peer) Addr() netip.Addr {
	return p.AddrPort.Addr().Unmap()
}

// MarshalZerologObject writes fields into zerolog event
func (p Peer) MarshalZerologObject(e *zerolog.Event) {
	e.Stringer("id", p.ID).
		Stringer("address", p.Addr()).
		Uint16("port", p.Port())
}

// ClientError represents an error that should be exposed to the client over
// the BitTorrent protocol implementation.
type ClientError string

// Error implements the error interface for ClientError.
func (c ClientError) Error() string { return string(c) }