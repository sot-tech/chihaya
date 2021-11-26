// Package bittorrent implements all of the abstractions used to decouple the
// protocol of a BitTorrent tracker from the logic of handling Announces and
// Scrapes.
package bittorrent

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/chihaya/chihaya/pkg/log"
	"github.com/pkg/errors"
	"net"
	"time"
)

// PeerIDLen is length of peer id field in bytes
const PeerIDLen = 20

// PeerID represents a peer ID.
type PeerID [PeerIDLen]byte

// InvalidPeerIDSizeError holds error about invalid PeerID size
var InvalidPeerIDSizeError = errors.New("peer ID must be 20 bytes")

// NewPeerID creates a PeerID from a byte slice.
//
// It panics if b is not 20 bytes long.
func NewPeerID(b []byte) (PeerID, error) {
	var p PeerID
	if len(b) != PeerIDLen {
		return p, InvalidPeerIDSizeError
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
	InfoHashV1Len          = sha1.Size
	// InfoHashV2Len ... sha256.Size
	InfoHashV2Len          = sha256.Size
	// NoneInfoHash dummy invalid InfoHash
	NoneInfoHash  InfoHash = ""
)

var (
	// InvalidHashTypeError holds error about invalid InfoHash input type
	InvalidHashTypeError = errors.New("info hash must be provided as byte slice or raw/hex string")
	// InvalidHashSizeError holds error about invalid InfoHash size
	InvalidHashSizeError = errors.New("info hash must be either 20 (for torrent V1) or 32 (V2) bytes")
)

// TruncateV1 returns truncated to 20-bytes length array of the corresponding InfoHash.
// If InfoHash is V2 (32 bytes), it will be truncated to 20 bytes
// according to BEP52.
func (i InfoHash) TruncateV1() InfoHash {
	return i[:InfoHashV1Len]
}

// NewInfoHash creates an InfoHash from a byte slice or raw/hex string.
func NewInfoHash(b interface{}) (InfoHash, error) {
	if b == nil {
		return NoneInfoHash, InvalidHashTypeError
	}
	var ba []byte
	switch t := b.(type) {
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
		return NoneInfoHash, InvalidHashSizeError
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

// AnnounceRequest represents the parsed parameters from an announce request.
type AnnounceRequest struct {
	Event           Event
	InfoHash        InfoHash
	Compact         bool
	EventProvided   bool
	NumWantProvided bool
	IPProvided      bool
	NumWant         uint32
	Left            uint64
	Downloaded      uint64
	Uploaded        uint64

	Peer
	Params
}

// LogFields renders the current response as a set of log fields.
func (r AnnounceRequest) LogFields() log.Fields {
	return log.Fields{
		"event":           r.Event,
		"infoHash":        r.InfoHash,
		"compact":         r.Compact,
		"eventProvided":   r.EventProvided,
		"numWantProvided": r.NumWantProvided,
		"ipProvided":      r.IPProvided,
		"numWant":         r.NumWant,
		"left":            r.Left,
		"downloaded":      r.Downloaded,
		"uploaded":        r.Uploaded,
		"peer":            r.Peer,
		"params":          r.Params,
	}
}

// AnnounceResponse represents the parameters used to create an announce
// response.
type AnnounceResponse struct {
	Compact     bool
	Complete    uint32
	Incomplete  uint32
	Interval    time.Duration
	MinInterval time.Duration
	IPv4Peers   []Peer
	IPv6Peers   []Peer
}

// LogFields renders the current response as a set of log fields.
func (r AnnounceResponse) LogFields() log.Fields {
	return log.Fields{
		"compact":     r.Compact,
		"complete":    r.Complete,
		"interval":    r.Interval,
		"minInterval": r.MinInterval,
		"ipv4Peers":   r.IPv4Peers,
		"ipv6Peers":   r.IPv6Peers,
	}
}

// ScrapeRequest represents the parsed parameters from a scrape request.
type ScrapeRequest struct {
	AddressFamily AddressFamily
	InfoHashes    []InfoHash
	Params        Params
}

// LogFields renders the current response as a set of log fields.
func (r ScrapeRequest) LogFields() log.Fields {
	return log.Fields{
		"addressFamily": r.AddressFamily,
		"infoHashes":    r.InfoHashes,
		"params":        r.Params,
	}
}

// ScrapeResponse represents the parameters used to create a scrape response.
//
// The Scrapes must be in the same order as the InfoHashes in the corresponding
// ScrapeRequest.
type ScrapeResponse struct {
	Files []Scrape
}

// LogFields renders the current response as a set of Logrus fields.
func (sr ScrapeResponse) LogFields() log.Fields {
	return log.Fields{
		"files": sr.Files,
	}
}

// Scrape represents the state of a swarm that is returned in a scrape response.
type Scrape struct {
	InfoHash   InfoHash
	Snatches   uint32
	Complete   uint32
	Incomplete uint32
}

// AddressFamily is the address family of an IP address.
type AddressFamily uint8

func (af AddressFamily) String() string {
	switch af {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	default:
		panic("tried to print unknown AddressFamily")
	}
}

// AddressFamily constants.
const (
	IPv4 AddressFamily = iota
	IPv6
)

// IP is a net.IP with an AddressFamily.
type IP struct {
	net.IP
	AddressFamily
}

func (ip IP) String() string {
	return ip.IP.String()
}

// Peer represents the connection details of a peer that is returned in an
// announce response.
type Peer struct {
	ID   PeerID
	IP   IP
	Port uint16
}

// String implements fmt.Stringer to return a human-readable representation.
// The string will have the format <PeerID>@[<IP>]:<port>, for example
// "0102030405060708090a0b0c0d0e0f1011121314@[10.11.12.13]:1234"
func (p Peer) String() string {
	return fmt.Sprintf("%s@[%s]:%d", p.ID.String(), p.IP.String(), p.Port)
}

// LogFields renders the current peer as a set of Logrus fields.
func (p Peer) LogFields() log.Fields {
	return log.Fields{
		"ID":   p.ID,
		"IP":   p.IP,
		"port": p.Port,
	}
}

// Equal reports whether p and x are the same.
func (p Peer) Equal(x Peer) bool { return p.EqualEndpoint(x) && p.ID == x.ID }

// EqualEndpoint reports whether p and x have the same endpoint.
func (p Peer) EqualEndpoint(x Peer) bool { return p.Port == x.Port && p.IP.Equal(x.IP.IP) }

// ClientError represents an error that should be exposed to the client over
// the BitTorrent protocol implementation.
type ClientError string

// Error implements the error interface for ClientError.
func (c ClientError) Error() string { return string(c) }
