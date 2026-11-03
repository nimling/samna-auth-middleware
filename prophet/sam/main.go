package sam

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	disciplesam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	"github.com/nimling/samna-auth-middleware/v2/notification"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const leadInterval = 5 * time.Second

var (
	ErrNotStarted = errors.New("prophet runtime not started: call sam.Init")
	ErrStarted    = errors.New("prophet runtime already started")
)

type runner struct {
	wg   sync.WaitGroup
	done chan struct{}
}

var (
	runMu sync.Mutex
	run   *runner
)

func Init(ctx context.Context) error {
	db := sdb.Instance()
	if db == nil {
		return shared.ErrNotConnected
	}
	r, err := start(ctx)
	if err != nil {
		return err
	}
	if err := notification.Init(ctx, db); err != nil {
		return err
	}
	r.run(func() { <-notification.Done() })
	if err := analytics.Init(ctx, db); err != nil {
		return err
	}
	r.run(func() { <-analytics.Done() })
	r.run(func() {
		shared.LeadSync(ctx, db, shared.RetentionLockKey, leadInterval, db.PruneLoop)
	})
	if utils.GetDebugMode() != utils.DebugModeNone {
		return nil
	}
	r.run(func() {
		started := make(map[uuid.UUID]bool)
		for {
			var peers []shared.ProphetPeer
			if err := db.NewSelect().Model(&peers).Scan(ctx); err != nil && ctx.Err() == nil {
				db.Logger.Errorf("peer sync: read prophet_peer: %v", err)
			}
			for i := range peers {
				if started[peers[i].ID] {
					continue
				}
				started[peers[i].ID] = true
				replica := &disciplesam.Replica{DB: db, Peer: &peers[i]}
				r.run(func() {
					shared.LeadSync(ctx, db, "claimius.prophet_peer:"+replica.Peer.ID.String(), leadInterval, replica.Pull)
				})
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(leadInterval):
			}
		}
	})
	return nil
}

func start(ctx context.Context) (*runner, error) {
	runMu.Lock()
	defer runMu.Unlock()
	if run != nil && !run.ended() {
		return nil, ErrStarted
	}
	r := &runner{done: make(chan struct{})}
	r.wg.Add(1)
	go func() {
		<-ctx.Done()
		r.wg.Done()
		r.wg.Wait()
		close(r.done)
	}()
	run = r
	return r, nil
}

func (r *runner) run(work func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		work()
	}()
}

func (r *runner) ended() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func Done() <-chan struct{} {
	runMu.Lock()
	defer runMu.Unlock()
	if run == nil {
		log.Error(ErrNotStarted)
		done := make(chan struct{})
		close(done)
		return done
	}
	return run.done
}
