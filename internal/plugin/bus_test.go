package plugin

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
)

func TestBusPriorityOrder(t *testing.T) {
	b := NewBus(quietLogger())
	var got []string
	reg := []struct {
		name string
		prio api.Priority
	}{
		{"monitor", api.Monitor},
		{"normal1", api.Normal},
		{"lowest", api.Lowest},
		{"high", api.High},
		{"normal2", api.Normal},
		{"low", api.Low},
		{"highest", api.Highest},
	}
	for _, r := range reg {
		name := r.name
		if _, err := Subscribe(b, r.prio, false, func(*api.PlayerJoinEvent) { got = append(got, name) }); err != nil {
			t.Fatal(err)
		}
	}
	b.Dispatch(&api.PlayerJoinEvent{})
	want := []string{"lowest", "low", "normal1", "normal2", "high", "highest", "monitor"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestBusCancellation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(b *Bus, log *[]string)
		wantLog []string
		wantCxl bool
	}{
		{
			name: "ignoreCancelled skips after cancel",
			setup: func(b *Bus, log *[]string) {
				Subscribe(b, api.Low, false, func(e *api.BlockBreakEvent) { e.SetCancelled(true); *log = append(*log, "cancel") })
				Subscribe(b, api.Normal, true, func(e *api.BlockBreakEvent) { *log = append(*log, "skipped") })
				Subscribe(b, api.Monitor, false, func(e *api.BlockBreakEvent) { *log = append(*log, "monitor") })
			},
			wantLog: []string{"cancel", "monitor"},
			wantCxl: true,
		},
		{
			name: "uncancel re-enables ignoreCancelled handlers",
			setup: func(b *Bus, log *[]string) {
				Subscribe(b, api.Lowest, false, func(e *api.BlockBreakEvent) { e.SetCancelled(true) })
				Subscribe(b, api.Low, false, func(e *api.BlockBreakEvent) { e.SetCancelled(false) })
				Subscribe(b, api.High, true, func(e *api.BlockBreakEvent) { *log = append(*log, "ran") })
			},
			wantLog: []string{"ran"},
		},
		{
			name: "panic is recovered and dispatch continues",
			setup: func(b *Bus, log *[]string) {
				Subscribe(b.For("bad"), api.Low, false, func(e *api.BlockBreakEvent) { panic("boom") })
				Subscribe(b, api.Normal, false, func(e *api.BlockBreakEvent) { *log = append(*log, "after") })
			},
			wantLog: []string{"after"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBus(quietLogger())
			var log []string
			tt.setup(b, &log)
			e := Fire(b, &api.BlockBreakEvent{})
			if !slices.Equal(log, tt.wantLog) {
				t.Errorf("log = %v, want %v", log, tt.wantLog)
			}
			if e.Cancelled() != tt.wantCxl {
				t.Errorf("cancelled = %v, want %v", e.Cancelled(), tt.wantCxl)
			}
		})
	}
}

func TestBusPanicAttribution(t *testing.T) {
	b := NewBus(quietLogger())
	var owner string
	var kind api.EventKind
	b.OnPanic = func(o string, k api.EventKind, _ any) { owner, kind = o, k }
	Subscribe(b.For("faulty"), api.Normal, false, func(*api.PlayerQuitEvent) { panic("x") })
	b.Dispatch(&api.PlayerQuitEvent{})
	if owner != "faulty" || kind != api.KindPlayerQuit {
		t.Fatalf("OnPanic got (%q, %v)", owner, kind)
	}
}

func TestBusUnsubscribe(t *testing.T) {
	b := NewBus(quietLogger())
	var n atomic.Int32
	s1, _ := Subscribe(b.For("a"), api.Normal, false, func(*api.PlayerJoinEvent) { n.Add(1) })
	Subscribe(b.For("a"), api.Normal, false, func(*api.PlayerQuitEvent) { n.Add(10) })
	Subscribe(b.For("b"), api.Normal, false, func(*api.PlayerJoinEvent) { n.Add(100) })

	if !b.Unsubscribe(s1) || b.Unsubscribe(s1) {
		t.Fatal("unsubscribe should succeed exactly once")
	}
	b.Dispatch(&api.PlayerJoinEvent{})
	if n.Load() != 100 {
		t.Fatalf("n = %d after unsubscribe", n.Load())
	}
	if removed := b.UnsubscribeAll("a"); removed != 1 {
		t.Fatalf("UnsubscribeAll removed %d", removed)
	}
	if b.HasHandlers(api.KindPlayerQuit) {
		t.Fatal("quit handlers should be gone")
	}
}

func TestBusSubscribeDuringDispatch(t *testing.T) {
	b := NewBus(quietLogger())
	calls := 0
	Subscribe(b, api.Normal, false, func(*api.PlayerJoinEvent) {
		calls++
		Subscribe(b, api.Normal, false, func(*api.PlayerJoinEvent) { calls += 100 })
	})
	b.Dispatch(&api.PlayerJoinEvent{})
	if calls != 1 {
		t.Fatalf("handler added during dispatch ran in the same dispatch: %d", calls)
	}
}

type customEvent struct{ n int }

var customKind api.EventKind

func (*customEvent) Kind() api.EventKind { return customKind }
func (*customEvent) Async() bool         { return false }

func TestBusCustomKind(t *testing.T) {
	b := NewBus(quietLogger())
	k, err := b.RegisterKind("test:custom")
	if err != nil {
		t.Fatal(err)
	}
	if k2, _ := b.RegisterKind("test:custom"); k2 != k || k < api.KindCustom {
		t.Fatalf("kinds %d %d", k, k2)
	}
	customKind = k
	Subscribe(b, api.Normal, false, func(e *customEvent) { e.n++ })
	if e := Fire(b, &customEvent{}); e.n != 1 {
		t.Fatal("custom event not delivered")
	}
}

func TestBusInvalidSubscription(t *testing.T) {
	b := NewBus(quietLogger())
	if _, err := SubscribeKind(b, api.KindInvalid, api.Normal, false, func(api.Event) {}); err == nil {
		t.Error("invalid kind accepted")
	}
	if _, err := SubscribeKind(b, api.KindPlayerJoin, api.Monitor+1, false, func(api.Event) {}); err == nil {
		t.Error("invalid priority accepted")
	}
}

func TestBusConcurrent(t *testing.T) {
	b := NewBus(quietLogger())
	var hits atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					b.Dispatch(&api.PlayerMoveEvent{})
				}
			}
		}()
	}
	var subs []Subscription
	for i := 0; i < 200; i++ {
		s, err := Subscribe(b.For("p"), api.Priority(i%6), i%2 == 0, func(*api.PlayerMoveEvent) { hits.Add(1) })
		if err != nil {
			t.Fatal(err)
		}
		subs = append(subs, s)
		if i%3 == 0 {
			b.Unsubscribe(subs[i/2])
		}
	}
	b.UnsubscribeAll("p")
	close(stop)
	wg.Wait()
	if b.HasHandlers(api.KindPlayerMove) {
		t.Fatal("handlers left after UnsubscribeAll")
	}
	done := make(chan struct{})
	b.DispatchAsync(&api.PlayerPreLoginEvent{}, func(api.Event) { close(done) })
	<-done
}

func BenchmarkBusDispatch(b *testing.B) {
	bus := NewBus(quietLogger())
	for p := api.Lowest; p <= api.Monitor; p++ {
		Subscribe(bus, p, true, func(e *api.PlayerMoveEvent) {})
	}
	e := &api.PlayerMoveEvent{}
	b.ReportAllocs()
	for b.Loop() {
		bus.Dispatch(e)
	}
}
