package plugin

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/api"
)

// MaxEventKinds bounds built-in plus custom event kinds.
const MaxEventKinds = 2048

type handler struct {
	id              uint64
	owner           string
	priority        api.Priority
	ignoreCancelled bool
	fn              func(api.Event)
}

// Bus dispatches events to handlers sorted by priority then registration
// order. Handler slices are immutable snapshots replaced on subscribe, so
// dispatch is lock-free and allocation-free.
type Bus struct {
	slots [MaxEventKinds]atomic.Pointer[[]handler]

	mu       sync.Mutex
	nextID   uint64
	nextKind api.EventKind
	custom   map[string]api.EventKind

	log *slog.Logger
	// OnPanic is called after a handler panic has been recovered and logged.
	OnPanic func(owner string, kind api.EventKind, v any)
}

func NewBus(log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
	return &Bus{log: log, nextKind: api.KindCustom, custom: map[string]api.EventKind{}}
}

// Subscription identifies a registered handler.
type Subscription struct {
	id   uint64
	kind api.EventKind
}

// Registrar is either a *Bus (owner "") or an owner-scoped view from Bus.For.
type Registrar interface {
	register(kind api.EventKind, h handler) (Subscription, error)
}

type scoped struct {
	bus   *Bus
	owner string
}

func (s scoped) register(kind api.EventKind, h handler) (Subscription, error) {
	h.owner = s.owner
	return s.bus.register(kind, h)
}

// For returns a registrar that attributes handlers to owner.
func (b *Bus) For(owner string) Registrar { return scoped{b, owner} }

var ErrBadKind = errors.New("plugin: invalid event kind")

// Subscribe registers fn for events of type E. E must be a pointer type whose
// Kind method does not dereference the receiver.
func Subscribe[E api.Event](r Registrar, priority api.Priority, ignoreCancelled bool, fn func(E)) (Subscription, error) {
	var zero E
	return r.register(zero.Kind(), handler{
		priority:        priority,
		ignoreCancelled: ignoreCancelled,
		fn:              func(e api.Event) { fn(e.(E)) },
	})
}

// SubscribeKind registers an untyped handler, used by runtimes that receive
// events by kind number.
func SubscribeKind(r Registrar, kind api.EventKind, priority api.Priority, ignoreCancelled bool, fn func(api.Event)) (Subscription, error) {
	return r.register(kind, handler{priority: priority, ignoreCancelled: ignoreCancelled, fn: fn})
}

func (b *Bus) register(kind api.EventKind, h handler) (Subscription, error) {
	if kind == api.KindInvalid || kind >= MaxEventKinds {
		return Subscription{}, fmt.Errorf("%w: %d", ErrBadKind, kind)
	}
	if h.priority > api.Monitor {
		return Subscription{}, fmt.Errorf("plugin: invalid priority %d", h.priority)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	h.id = b.nextID
	var old []handler
	if p := b.slots[kind].Load(); p != nil {
		old = *p
	}
	i := len(old)
	for i > 0 && old[i-1].priority > h.priority {
		i--
	}
	next := slices.Insert(slices.Clone(old), i, h)
	b.slots[kind].Store(&next)
	return Subscription{id: h.id, kind: kind}, nil
}

func (b *Bus) Unsubscribe(s Subscription) bool {
	if s.kind >= MaxEventKinds {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.removeLocked(s.kind, func(h *handler) bool { return h.id == s.id }) > 0
}

// UnsubscribeAll removes every handler registered by owner.
func (b *Bus) UnsubscribeAll(owner string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for k := range b.slots {
		n += b.removeLocked(api.EventKind(k), func(h *handler) bool { return h.owner == owner })
	}
	return n
}

func (b *Bus) removeLocked(kind api.EventKind, match func(*handler) bool) int {
	p := b.slots[kind].Load()
	if p == nil {
		return 0
	}
	next := make([]handler, 0, len(*p))
	for i := range *p {
		if !match(&(*p)[i]) {
			next = append(next, (*p)[i])
		}
	}
	removed := len(*p) - len(next)
	if removed == 0 {
		return 0
	}
	if len(next) == 0 {
		b.slots[kind].Store(nil)
	} else {
		b.slots[kind].Store(&next)
	}
	return removed
}

// HasHandlers lets callers skip building events nobody listens to.
func (b *Bus) HasHandlers(kind api.EventKind) bool {
	return kind < MaxEventKinds && b.slots[kind].Load() != nil
}

// RegisterKind allocates a kind for a custom event type. Registering the same
// name twice returns the same kind.
func (b *Bus) RegisterKind(name string) (api.EventKind, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if k, ok := b.custom[name]; ok {
		return k, nil
	}
	if b.nextKind >= MaxEventKinds {
		return 0, errors.New("plugin: too many custom event kinds")
	}
	k := b.nextKind
	b.nextKind++
	b.custom[name] = k
	return k, nil
}

// Dispatch runs all handlers on the calling goroutine and returns the event.
// For cancellable events, handlers registered with ignoreCancelled are skipped
// once the event is cancelled.
func (b *Bus) Dispatch(e api.Event) api.Event {
	k := e.Kind()
	if k >= MaxEventKinds {
		return e
	}
	p := b.slots[k].Load()
	if p == nil {
		return e
	}
	c, cancellable := e.(api.Cancellable)
	hs := *p
	for i := range hs {
		if cancellable && hs[i].ignoreCancelled && c.Cancelled() {
			continue
		}
		b.invoke(&hs[i], e)
	}
	return e
}

// Fire dispatches a typed event and returns it, for call sites that want the
// concrete type back.
func Fire[E api.Event](b *Bus, e E) E {
	b.Dispatch(e)
	return e
}

// DispatchAsync runs Dispatch on a new goroutine and then calls done there.
// Use it for async events (pre-login, chat) from goroutines that must not
// block.
func (b *Bus) DispatchAsync(e api.Event, done func(api.Event)) {
	go func() {
		b.Dispatch(e)
		if done != nil {
			done(e)
		}
	}()
}

func (b *Bus) invoke(h *handler, e api.Event) {
	defer func() {
		if v := recover(); v != nil {
			b.log.Error("event handler panicked",
				"plugin", h.owner, "event", e.Kind().String(), "panic", v, "stack", string(debug.Stack()))
			if b.OnPanic != nil {
				b.OnPanic(h.owner, e.Kind(), v)
			}
		}
	}()
	h.fn(e)
}
