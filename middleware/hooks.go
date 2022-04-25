package middleware

import (
	"context"
	"errors"

	"github.com/sot-tech/mochi/bittorrent"
	"github.com/sot-tech/mochi/storage"
)

// Hook abstracts the concept of anything that needs to interact with a
// BitTorrent client's request and response to a BitTorrent tracker.
// PreHooks and PostHooks both use the same interface.
//
// A Hook can implement stop.Stopper if clean shutdown is required.
type Hook interface {
	HandleAnnounce(context.Context, *bittorrent.AnnounceRequest, *bittorrent.AnnounceResponse) (context.Context, error)
	HandleScrape(context.Context, *bittorrent.ScrapeRequest, *bittorrent.ScrapeResponse) (context.Context, error)
}

type skipSwarmInteraction struct{}

// SkipSwarmInteractionKey is a key for the context of an Announce to control
// whether the swarm interaction middleware should run.
// Any non-nil value set for this key will cause the swarm interaction
// middleware to skip.
var SkipSwarmInteractionKey = skipSwarmInteraction{}

type swarmInteractionHook struct {
	store storage.PeerStorage
}

func (h *swarmInteractionHook) HandleAnnounce(ctx context.Context, req *bittorrent.AnnounceRequest, _ *bittorrent.AnnounceResponse) (outCtx context.Context, err error) {
	outCtx = ctx
	if ctx.Value(SkipSwarmInteractionKey) != nil {
		return
	}

	var storeFn func(bittorrent.InfoHash, bittorrent.Peer) error

	switch {
	case req.Event == bittorrent.Stopped:
		storeFn = func(hash bittorrent.InfoHash, peer bittorrent.Peer) error {
			err = h.store.DeleteSeeder(hash, peer)
			if err != nil && !errors.Is(err, storage.ErrResourceDoesNotExist) {
				return err
			}

			err = h.store.DeleteLeecher(hash, peer)
			if err != nil && !errors.Is(err, storage.ErrResourceDoesNotExist) {
				return err
			}
			return nil
		}
	case req.Event == bittorrent.Completed:
		storeFn = h.store.GraduateLeecher
	case req.Left == 0:
		// Completed events will also have Left == 0, but by making this
		// an extra case we can treat "old" seeders differently from
		// graduating leechers. (Calling PutSeeder is probably faster
		// than calling GraduateLeecher.)
		storeFn = h.store.PutSeeder
	default:
		storeFn = h.store.PutLeecher
	}

	if err = storeFn(req.InfoHash, req.Peer); err == nil && len(req.InfoHash) == bittorrent.InfoHashV2Len {
		err = storeFn(req.InfoHash.TruncateV1(), req.Peer)
	}

	return
}

func (h *swarmInteractionHook) HandleScrape(ctx context.Context, _ *bittorrent.ScrapeRequest, _ *bittorrent.ScrapeResponse) (context.Context, error) {
	// Scrapes have no effect on the swarm.
	return ctx, nil
}

type skipResponseHook struct{}

// SkipResponseHookKey is a key for the context of an Announce or Scrape to
// control whether the response middleware should run.
// Any non-nil value set for this key will cause the response middleware to
// skip.
var SkipResponseHookKey = skipResponseHook{}

// type scrapeAddressType struct{}

// ScrapeIsIPv6Key is the key under which to store whether or not the
// address used to request a scrape was an IPv6 address.
// The value is expected to be of type bool.
// A missing value or a value that is not a bool for this key is equivalent to
// it being set to false.
// var ScrapeIsIPv6Key = scrapeAddressType{}

type responseHook struct {
	store storage.PeerStorage
}

func (h *responseHook) HandleAnnounce(ctx context.Context, req *bittorrent.AnnounceRequest, resp *bittorrent.AnnounceResponse) (_ context.Context, err error) {
	if ctx.Value(SkipResponseHookKey) != nil {
		return ctx, nil
	}

	// Add the Scrape data to the response.
	resp.Incomplete, resp.Complete, _ = h.store.ScrapeSwarm(req.InfoHash)
	if len(req.InfoHash) == bittorrent.InfoHashV2Len {
		incomplete, complete, _ := h.store.ScrapeSwarm(req.InfoHash.TruncateV1())
		resp.Incomplete, resp.Complete = resp.Incomplete+incomplete, resp.Complete+complete
	}

	err = h.appendPeers(req, resp)
	return ctx, err
}

func (h *responseHook) appendPeers(req *bittorrent.AnnounceRequest, resp *bittorrent.AnnounceResponse) error {
	seeding := req.Left == 0
	max := int(req.NumWant)
	storePeers, err := h.store.AnnouncePeers(req.InfoHash, seeding, max, req.Peer)
	if err != nil && !errors.Is(err, storage.ErrResourceDoesNotExist) {
		return err
	}
	err = nil
	peers := make([]bittorrent.Peer, 0, len(resp.IPv4Peers)+len(resp.IPv6Peers)+len(storePeers))

	// append peers, which added in middleware
	if req.Peer.Addr().Is6() {
		peers = append(peers, resp.IPv6Peers...)
		peers = append(peers, resp.IPv4Peers...)
	} else {
		peers = append(peers, resp.IPv4Peers...)
		peers = append(peers, resp.IPv6Peers...)
	}
	peers = append(peers, storePeers...)

	// Some clients expect a minimum of their own peer representation returned to
	// them if they are the only peer in a swarm.
	if len(peers) == 0 {
		if seeding {
			resp.Complete++
		} else {
			resp.Incomplete++
		}
		peers = append(peers, req.Peer)
	}

	uniquePeers := make(map[bittorrent.Peer]interface{}, len(peers))

	resp.IPv4Peers = make([]bittorrent.Peer, 0, len(peers)/2)
	resp.IPv6Peers = make([]bittorrent.Peer, 0, len(peers)/2)

	for _, p := range peers {
		if err != nil || len(uniquePeers) > max {
			break
		}
		if _, found := uniquePeers[p]; !found {
			uniquePeers[p] = nil
			if p.Addr().Is6() {
				resp.IPv6Peers = append(resp.IPv6Peers, p)
			} else if p.Addr().Is4() {
				resp.IPv4Peers = append(resp.IPv4Peers, p)
			} else {
				err = bittorrent.ErrInvalidIP
			}
		}
	}

	return err
}

func (h *responseHook) HandleScrape(ctx context.Context, req *bittorrent.ScrapeRequest, resp *bittorrent.ScrapeResponse) (context.Context, error) {
	if ctx.Value(SkipResponseHookKey) != nil {
		return ctx, nil
	}

	for _, infoHash := range req.InfoHashes {
		scr := bittorrent.Scrape{InfoHash: infoHash}
		scr.Incomplete, scr.Complete, scr.Snatches = h.store.ScrapeSwarm(infoHash)
		if len(infoHash) == bittorrent.InfoHashV2Len {
			leechers, seeders, snatched := h.store.ScrapeSwarm(infoHash.TruncateV1())
			scr.Incomplete, scr.Complete, scr.Snatches = scr.Incomplete+leechers, scr.Complete+seeders, scr.Snatches+snatched
		}

		resp.Files = append(resp.Files, scr)
	}

	return ctx, nil
}
