package binding

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var errOperationRegistryClosing = errors.New("operation registry is shutting down")

type operation struct {
	cancel context.CancelFunc
	done   chan struct{}
	scope  string
}

// operationRegistry owns uniqueness, cancellation, completion, and shutdown
// for one class of long-running operations.
type operationRegistry struct {
	mu            sync.Mutex
	active        map[string]*operation
	blockedScopes map[string]struct{}
	closing       bool
}

func newOperationRegistry() *operationRegistry {
	return &operationRegistry{active: map[string]*operation{}, blockedScopes: map[string]struct{}{}}
}

func (r *operationRegistry) begin(parent context.Context, id, scope string) (context.Context, func(), error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil, errors.New("operation id is required")
	}
	if parent == nil {
		parent = context.Background()
	}

	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return nil, nil, errOperationRegistryClosing
	}
	if _, blocked := r.blockedScopes[scope]; blocked {
		r.mu.Unlock()
		return nil, nil, errors.New("operation scope is closing: " + scope)
	}
	if _, exists := r.active[id]; exists {
		r.mu.Unlock()
		return nil, nil, errors.New("operation already in flight: " + id)
	}
	ctx, cancel := context.WithCancel(parent)
	op := &operation{cancel: cancel, done: make(chan struct{}), scope: scope}
	r.active[id] = op
	r.mu.Unlock()

	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			if r.active[id] == op {
				delete(r.active, id)
			}
			close(op.done)
			r.mu.Unlock()
		})
	}
	return ctx, finish, nil
}

func (r *operationRegistry) cancelScope(ctx context.Context, scope string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	r.blockedScopes[scope] = struct{}{}
	operations := make([]*operation, 0)
	for _, op := range r.active {
		if op.scope == scope {
			operations = append(operations, op)
		}
	}
	r.mu.Unlock()
	for _, op := range operations {
		op.cancel()
	}
	for _, op := range operations {
		select {
		case <-op.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (r *operationRegistry) resumeScope(scope string) {
	r.mu.Lock()
	delete(r.blockedScopes, scope)
	r.mu.Unlock()
}

func (r *operationRegistry) cancel(id string) {
	r.mu.Lock()
	op := r.active[id]
	r.mu.Unlock()
	if op != nil {
		op.cancel()
	}
}

func (r *operationRegistry) shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	r.closing = true
	operations := make([]*operation, 0, len(r.active))
	for _, op := range r.active {
		operations = append(operations, op)
	}
	r.mu.Unlock()

	for _, op := range operations {
		op.cancel()
	}
	for _, op := range operations {
		select {
		case <-op.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Shutdown stops long-running binding operations in dependency order.
// Runner operations are drained before standalone requests because a Runner
// owns its current request through the parent context.
func Shutdown(ctx context.Context, apis ...any) error {
	var firstErr error
	for _, api := range apis {
		if runner, ok := api.(*RunnerApi); ok {
			if err := runner.shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	for _, api := range apis {
		if request, ok := api.(*RequestApi); ok {
			if err := request.shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
