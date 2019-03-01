package client

import (
	"context"
	"time"

	"github.com/pkg/errors"

	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/log"
	tmpubsub "github.com/tendermint/tendermint/libs/pubsub"
	tmquery "github.com/tendermint/tendermint/libs/pubsub/query"
	nm "github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/rpc/core"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	rpctypes "github.com/tendermint/tendermint/rpc/lib/types"
	"github.com/tendermint/tendermint/types"
)

/*
Local is a Client implementation that directly executes the rpc
functions on a given node, without going through HTTP or GRPC.

This implementation is useful for:

* Running tests against a node in-process without the overhead
of going through an http server
* Communication between an ABCI app and Tendermint core when they
are compiled in process.

For real clients, you probably want to use client.HTTP.  For more
powerful control during testing, you probably want the "client/mock" package.
*/
type Local struct {
	*types.EventBus
	Logger log.Logger
}

// NewLocal configures a client that calls the Node directly.
//
// Note that given how rpc/core works with package singletons, that
// you can only have one node per process.  So make sure test cases
// don't run in parallel, or try to simulate an entire network in
// one process...
func NewLocal(node *nm.Node) *Local {
	node.ConfigureRPC()
	return &Local{
		EventBus: node.EventBus(),
		Logger:   log.NewNopLogger(),
	}
}

var (
	_ Client        = (*Local)(nil)
	_ NetworkClient = Local{}
	_ EventsClient  = (*Local)(nil)
)

// SetLogger allows to set a logger on the client.
func (c *Local) SetLogger(l log.Logger) {
	c.Logger = l
}

func (Local) Status() (*ctypes.ResultStatus, error) {
	return core.Status(&rpctypes.Context{})
}

func (Local) ABCIInfo() (*ctypes.ResultABCIInfo, error) {
	return core.ABCIInfo(&rpctypes.Context{})
}

func (c *Local) ABCIQuery(path string, data cmn.HexBytes) (*ctypes.ResultABCIQuery, error) {
	return c.ABCIQueryWithOptions(path, data, DefaultABCIQueryOptions)
}

func (Local) ABCIQueryWithOptions(path string, data cmn.HexBytes, opts ABCIQueryOptions) (*ctypes.ResultABCIQuery, error) {
	return core.ABCIQuery(&rpctypes.Context{}, path, data, opts.Height, opts.Prove)
}

func (Local) BroadcastTxCommit(tx types.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	return core.BroadcastTxCommit(&rpctypes.Context{}, tx)
}

func (Local) BroadcastTxAsync(tx types.Tx) (*ctypes.ResultBroadcastTx, error) {
	return core.BroadcastTxAsync(&rpctypes.Context{}, tx)
}

func (Local) BroadcastTxSync(tx types.Tx) (*ctypes.ResultBroadcastTx, error) {
	return core.BroadcastTxSync(&rpctypes.Context{}, tx)
}

func (Local) UnconfirmedTxs(limit int) (*ctypes.ResultUnconfirmedTxs, error) {
	return core.UnconfirmedTxs(&rpctypes.Context{}, limit)
}

func (Local) NumUnconfirmedTxs() (*ctypes.ResultUnconfirmedTxs, error) {
	return core.NumUnconfirmedTxs(&rpctypes.Context{})
}

func (Local) NetInfo() (*ctypes.ResultNetInfo, error) {
	return core.NetInfo(&rpctypes.Context{})
}

func (Local) DumpConsensusState() (*ctypes.ResultDumpConsensusState, error) {
	return core.DumpConsensusState(&rpctypes.Context{})
}

func (Local) ConsensusState() (*ctypes.ResultConsensusState, error) {
	return core.ConsensusState(&rpctypes.Context{})
}

func (Local) Health() (*ctypes.ResultHealth, error) {
	return core.Health(&rpctypes.Context{})
}

func (Local) DialSeeds(seeds []string) (*ctypes.ResultDialSeeds, error) {
	return core.UnsafeDialSeeds(&rpctypes.Context{}, seeds)
}

func (Local) DialPeers(peers []string, persistent bool) (*ctypes.ResultDialPeers, error) {
	return core.UnsafeDialPeers(&rpctypes.Context{}, peers, persistent)
}

func (Local) BlockchainInfo(minHeight, maxHeight int64) (*ctypes.ResultBlockchainInfo, error) {
	return core.BlockchainInfo(&rpctypes.Context{}, minHeight, maxHeight)
}

func (Local) Genesis() (*ctypes.ResultGenesis, error) {
	return core.Genesis(&rpctypes.Context{})
}

func (Local) Block(height *int64) (*ctypes.ResultBlock, error) {
	return core.Block(&rpctypes.Context{}, height)
}

func (Local) BlockResults(height *int64) (*ctypes.ResultBlockResults, error) {
	return core.BlockResults(&rpctypes.Context{}, height)
}

func (Local) Commit(height *int64) (*ctypes.ResultCommit, error) {
	return core.Commit(&rpctypes.Context{}, height)
}

func (Local) Validators(height *int64) (*ctypes.ResultValidators, error) {
	return core.Validators(&rpctypes.Context{}, height)
}

func (Local) Tx(hash []byte, prove bool) (*ctypes.ResultTx, error) {
	return core.Tx(&rpctypes.Context{}, hash, prove)
}

func (Local) TxSearch(query string, prove bool, page, perPage int) (*ctypes.ResultTxSearch, error) {
	return core.TxSearch(&rpctypes.Context{}, query, prove, page, perPage)
}

// Subscribe implements EventsClient by using local eventBus to subscribe given
// subscriber to query.By default, returns a channel with cap=1. Error is
// returned if it fails to subscribe.
// Channel is never closed to prevent clients from seeing an erroneus event.
func (c *Local) Subscribe(ctx context.Context, subscriber, query string, outCapacity ...int) (out <-chan ctypes.ResultEvent, err error) {
	q, err := tmquery.New(query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse query")
	}
	sub, err := c.EventBus.Subscribe(ctx, subscriber, q)
	if err != nil {
		return nil, errors.Wrap(err, "failed to subscribe")
	}

	outCap := 1
	if len(outCapacity) > 0 {
		outCap = outCapacity[0]
	}

	outc := make(chan ctypes.ResultEvent, outCap)
	go func(sub types.Subscription) {
		for {
			select {
			case msg := <-sub.Out():
				result := ctypes.ResultEvent{Query: query, Data: msg.Data(), Tags: msg.Tags()}
				if cap(outc) == 0 {
					outc <- result
				} else {
					select {
					case outc <- result:
					default:
						c.Logger.Error("wanted to publish ResultEvent, but out channel is full", "result", result, "query", result.Query)
					}
				}
			case <-sub.Cancelled():
				if sub.Err() != tmpubsub.ErrUnsubscribed {
					// resubscribe
					c.Logger.Error("subscription was cancelled, resubscribing...", "err", err, "query", query)
					var err error
					for {
						if !c.IsRunning() {
							return
						}

						ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
						defer cancel()
						sub, err = c.EventBus.Subscribe(ctx, subscriber, q)
						if err == nil {
							break
						}
					}
				}
				return
			case <-c.Quit():
				return
			}
		}
	}(sub)

	return outc, nil
}

// Unsubscribe implements EventsClient by using local eventBus to unsubscribe
// given subscriber from query.
func (c *Local) Unsubscribe(ctx context.Context, subscriber, query string) error {
	q, err := tmquery.New(query)
	if err != nil {
		return errors.Wrap(err, "failed to parse query")
	}
	return c.EventBus.Unsubscribe(ctx, subscriber, q)
}

// UnsubscribeAll implements EventsClient by using local eventBus to
// unsubscribe given subscriber from all queries.
func (c *Local) UnsubscribeAll(ctx context.Context, subscriber string) error {
	return c.EventBus.UnsubscribeAll(ctx, subscriber)
}
