package cnst

import "errors"

var (
	// ErrDuplicateToolName is returned when a tool name is duplicated
	ErrDuplicateToolName = errors.New("duplicate tool name")
	// ErrDuplicateServerName is returned when a server name is duplicated
	ErrDuplicateServerName = errors.New("duplicate server name")
	// ErrDuplicateRouterPrefix is returned when a router prefix is duplicated
	ErrDuplicateRouterPrefix = errors.New("duplicate router prefix")

	// ErrNotReceiver is returned when a notifier cannot receive updates
	ErrNotReceiver = errors.New("notifier cannot receive updates")
	// ErrNotSender is returned when a notifier cannot send updates
	ErrNotSender = errors.New("notifier cannot send updates")
)
