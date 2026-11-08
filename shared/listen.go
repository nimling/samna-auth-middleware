package shared

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lib/pq"
)

const (
	listenPing   = 90 * time.Second
	listenBuffer = 1024
)

var ErrNotConnected = errors.New("database not connected: call sdb.Init first")

type listen struct {
	mu       sync.Mutex
	listener *pq.Listener
	read     chan struct{}
	subs     map[string]map[chan *pq.Notification]struct{}
}

func (d *DB) Listen(ctx context.Context, channel string) (<-chan *pq.Notification, error) {
	if d == nil || d.listen == nil {
		return nil, ErrNotConnected
	}
	l := d.listen
	l.mu.Lock()
	if l.listener == nil {
		l.listener = pq.NewListener(d.ConnStr, time.Second, time.Minute, func(_ pq.ListenerEventType, err error) {
			if err != nil {
				d.Logger.Errorf("listener: %v", err)
			}
		})
		l.read = make(chan struct{})
		go l.readLoop(d, l.listener, l.read)
	}
	if len(l.subs[channel]) == 0 {
		if err := l.listener.Listen(channel); err != nil && !errors.Is(err, pq.ErrChannelAlreadyOpen) {
			listener, read := l.detach()
			l.mu.Unlock()
			closeListener(listener, read)
			return nil, err
		}
		l.subs[channel] = make(map[chan *pq.Notification]struct{})
	}
	ch := make(chan *pq.Notification, listenBuffer)
	l.subs[channel][ch] = struct{}{}
	l.mu.Unlock()
	go func() {
		<-ctx.Done()
		l.unlisten(d, channel, ch)
	}()
	return ch, nil
}

func (l *listen) unlisten(d *DB, channel string, ch chan *pq.Notification) {
	l.mu.Lock()
	delete(l.subs[channel], ch)
	if len(l.subs[channel]) == 0 {
		delete(l.subs, channel)
		if l.listener != nil {
			if err := l.listener.Unlisten(channel); err != nil {
				d.Logger.Errorf("listener: unlisten %s: %v", channel, err)
			}
		}
	}
	listener, read := l.detach()
	l.mu.Unlock()
	closeListener(listener, read)
	close(ch)
}

func (l *listen) detach() (*pq.Listener, chan struct{}) {
	if len(l.subs) > 0 || l.listener == nil {
		return nil, nil
	}
	listener, read := l.listener, l.read
	l.listener, l.read = nil, nil
	return listener, read
}

func closeListener(listener *pq.Listener, read chan struct{}) {
	if listener == nil {
		return
	}
	_ = listener.Close()
	<-read
}

func (l *listen) readLoop(d *DB, listener *pq.Listener, read chan struct{}) {
	defer close(read)
	ticker := time.NewTicker(listenPing)
	defer ticker.Stop()
	for {
		select {
		case n, ok := <-listener.Notify:
			if !ok {
				return
			}
			l.push(d, n)
		case <-ticker.C:
			go func() {
				if err := listener.Ping(); err != nil {
					d.Logger.Errorf("listener ping: %v", err)
				}
			}()
		}
	}
}

func (l *listen) push(d *DB, n *pq.Notification) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for channel, set := range l.subs {
		if n != nil && n.Channel != channel {
			continue
		}
		for ch := range set {
			select {
			case ch <- n:
			default:
				if n != nil {
					d.Logger.Errorf("listener: %s subscriber full, dropped", channel)
				}
			}
		}
	}
}
