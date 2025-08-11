package notifier

import (
	"context"
	"sync"

	"mcp-gateway/internal/common/config"

	"go.uber.org/zap"
)

// CompositeNotifier implements Notifier by combining multiple notifiers
type CompositeNotifier struct {
	logger    *zap.Logger
	notifiers []Notifier
	mu        sync.RWMutex
	watchers  map[chan<- *config.MCPConfig]struct{}
}

// NewCompositeNotifier creates a new composite notifier
func NewCompositeNotifier(ctx context.Context, logger *zap.Logger, notifiers ...Notifier) *CompositeNotifier {
	n := &CompositeNotifier{
		logger:    logger.Named("notifier.composite"),
		notifiers: notifiers,
		watchers:  make(map[chan<- *config.MCPConfig]struct{}),
	}

	// Start signal handler if can receive
	if n.CanReceive() {
		go n.watch(ctx)
	}

	return n
}

func (n *CompositeNotifier) watch(ctx context.Context) {
	// Start watching all underlying notifiers
	for _, notifier := range n.notifiers {
		if !notifier.CanReceive() {
			continue
		}

		notifierCh, err := notifier.Watch(ctx)
		if err != nil {
			n.logger.Error("failed to watch underlying notifier",
				zap.Error(err))
			continue
		}

		// Forward notifications from underlying notifiers
		go func(notifierCh <-chan *config.MCPConfig) {
			for {
				select {
				case cfg := <-notifierCh:
					n.notifyWatchers(cfg)
				case <-ctx.Done():
					return
				}
			}
		}(notifierCh)
	}
}

// notifyWatchers sends the config to all registered watchers
func (n *CompositeNotifier) notifyWatchers(cfg *config.MCPConfig) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	for watcher := range n.watchers {
		select {
		case watcher <- cfg:
		default:
			n.logger.Warn("watcher channel is full, skipping notification",
				zap.String("server", cfg.Name))
		}
	}
}

// Watch implements Notifier.Watch
func (n *CompositeNotifier) Watch(ctx context.Context) (<-chan *config.MCPConfig, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ch := make(chan *config.MCPConfig, 10)
	n.watchers[ch] = struct{}{}

	// Start a goroutine to handle context cancellation and cleanup
	go func() {
		<-ctx.Done()
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.watchers, ch)
		close(ch)
	}()

	return ch, nil
}

// NotifyUpdate implements Notifier.NotifyUpdate
func (n *CompositeNotifier) NotifyUpdate(ctx context.Context, server *config.MCPConfig) error {
	var lastErr error
	for _, notifier := range n.notifiers {
		if !notifier.CanSend() {
			continue
		}
		if err := notifier.NotifyUpdate(ctx, server); err != nil {
			lastErr = err
			n.logger.Error("failed to notify update",
				zap.Error(err))
		}
	}
	return lastErr
}

// CanReceive returns true if the notifier can receive updates
func (n *CompositeNotifier) CanReceive() bool {
	for _, notifier := range n.notifiers {
		if notifier.CanReceive() {
			return true
		}
	}
	return false
}

// CanSend returns true if the notifier can send updates
func (n *CompositeNotifier) CanSend() bool {
	for _, notifier := range n.notifiers {
		if notifier.CanSend() {
			return true
		}
	}
	return false
}
