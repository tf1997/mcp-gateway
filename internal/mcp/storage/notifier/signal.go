package notifier

import (
	"context"
	"mcp-gateway/internal/common/cnst"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"mcp-gateway/internal/common/config"
	"mcp-gateway/pkg/utils"

	"go.uber.org/zap"
)

// SignalNotifier implements Notifier using system signals
type SignalNotifier struct {
	logger   *zap.Logger
	pid      string
	mu       sync.RWMutex
	watchers map[chan *config.MCPConfig]struct{}
	role     config.NotifierRole
}

// NewSignalNotifier creates a new signal-based notifier
func NewSignalNotifier(ctx context.Context, logger *zap.Logger, pid string, role config.NotifierRole) *SignalNotifier {
	if logger == nil {
		panic("logger is required")
	}
	if pid == "" {
		panic("pid file path is required")
	}

	n := &SignalNotifier{
		logger:   logger.Named("notifier.signal"),
		pid:      pid,
		watchers: make(map[chan *config.MCPConfig]struct{}),
		role:     role,
	}

	// Start signal handler if can receive
	if n.CanReceive() {
		go n.handleSignals(ctx)
	}

	return n
}

// handleSignals listens for SIGHUP signals and notifies watchers
func (n *SignalNotifier) handleSignals(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	for {
		select {
		case sig := <-sigChan:
			n.logger.Info("Received reload signal", zap.String("signal", sig.String()))
			n.notifyWatchers()

		case <-ctx.Done():
			n.logger.Info("Signal handler stopped")
			return
		}
	}
}

// notifyWatchers sends nil config to all registered watchers
func (n *SignalNotifier) notifyWatchers() {
	n.mu.RLock()
	defer n.mu.RUnlock()

	for ch := range n.watchers {
		select {
		case ch <- nil:
			n.logger.Debug("Notified watcher")
		default:
			n.logger.Warn("Watcher channel is full, skipping")
		}
	}
}

// Watch implements Notifier.Watch
func (n *SignalNotifier) Watch(ctx context.Context) (<-chan *config.MCPConfig, error) {
	if !n.CanReceive() {
		return nil, cnst.ErrNotReceiver
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	ch := make(chan *config.MCPConfig, 10) // Buffered channel to prevent blocking
	n.watchers[ch] = struct{}{}

	// Cleanup on context cancellation
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
func (n *SignalNotifier) NotifyUpdate(_ context.Context, _ *config.MCPConfig) error {
	if !n.CanSend() {
		return cnst.ErrNotSender
	}

	if err := utils.SendSignalToPIDFile(n.pid, syscall.SIGHUP); err != nil {
		n.logger.Error("Failed to send signal", zap.Error(err))
		return err
	}
	n.logger.Info("Successfully sent SIGHUP signal")
	return nil
}

// CanReceive returns true if the notifier can receive updates
func (n *SignalNotifier) CanReceive() bool {
	return n.role == config.RoleReceiver || n.role == config.RoleBoth
}

// CanSend returns true if the notifier can send updates
func (n *SignalNotifier) CanSend() bool {
	return n.role == config.RoleSender || n.role == config.RoleBoth
}
