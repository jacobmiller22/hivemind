package plugins

import (
	"context"
	"io"
	"sync"

	"github.com/jacobmiller22/hivemind/pkg/daemon"
)

type EventPlugin interface {
	Name() string
	HandleEvent(ctx context.Context, eventName string, stdin io.Reader, stdout io.Writer) (*daemon.HivemindEvent, error)
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]EventPlugin)
)

func Register(p EventPlugin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.Name()] = p
}

func Get(name string) (EventPlugin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[name]
	return p, ok
}
