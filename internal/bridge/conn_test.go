package bridge

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestValueRoundTrip(t *testing.T) {
	u, _ := ParseUUID("069a79f4-44e9-4726-a5be-fca90e38aaf5")
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"true", true, true},
		{"false", false, false},
		{"int", 42, int64(42)},
		{"negative", int64(-7), int64(-7)},
		{"minint", int64(math.MinInt64), int64(math.MinInt64)},
		{"float", 1.5, 1.5},
		{"string", "héllo", "héllo"},
		{"empty string", "", ""},
		{"bytes", []byte{1, 2, 3}, []byte{1, 2, 3}},
		{"uuid", u, u},
		{"strings", []string{"a", "b"}, []any{"a", "b"}},
		{"list", []any{int64(1), "x", nil}, []any{int64(1), "x", nil}},
		{"map", map[string]any{"a": int64(1), "b": map[string]any{"c": true}}, map[string]any{"a": int64(1), "b": map[string]any{"c": true}}},
		{"location", Location{World: "w", X: 1, Y: 2, Z: 3, Yaw: 4, Pitch: 5}, map[string]any{"world": "w", "x": 1.0, "y": 2.0, "z": 3.0, "yaw": 4.0, "pitch": 5.0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := AppendFrame(nil, Frame{Kind: KindCall, ID: 9, Op: 77, Args: []any{tt.in}})
			if err != nil {
				t.Fatal(err)
			}
			f, _, err := ReadFrame(bytes.NewReader(buf), nil)
			if err != nil {
				t.Fatal(err)
			}
			if f.Kind != KindCall || f.ID != 9 || f.Op != 77 || len(f.Args) != 1 {
				t.Fatalf("header mismatch: %+v", f)
			}
			if !reflect.DeepEqual(f.Args[0], tt.want) {
				t.Fatalf("got %#v want %#v", f.Args[0], tt.want)
			}
		})
	}
}

func TestReadFrameRejectsGarbage(t *testing.T) {
	tests := map[string][]byte{
		"too large": {0x7f, 0xff, 0xff, 0xff},
		"short":     {0, 0, 0, 2, 1, 2},
		"bad tag":   {0, 0, 0, 8, KindNotify, 0, 0, 0, 0, 0, 1, 99},
		"truncated": {0, 0, 0, 9, KindNotify, 0, 0, 0, 0, 0, 1, TagString, 5},
		"huge list": {0, 0, 0, 10, KindNotify, 0, 0, 0, 0, 0, 1, TagList, 0xff, 0x7f},
	}
	for name, b := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ReadFrame(bytes.NewReader(b), nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestUUIDString(t *testing.T) {
	const s = "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	u, err := ParseUUID(s)
	if err != nil || u.String() != s {
		t.Fatalf("got %v %v", u, err)
	}
	if _, err := ParseUUID("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func pipePair(t *testing.T, ha, hb Handlers) (*Conn, *Conn) {
	t.Helper()
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	ca, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	a, b := NewConn(ca, ha), NewConn(<-accepted, hb)
	go a.Serve()
	go b.Serve()
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

func TestConnBidirectional(t *testing.T) {
	var mu sync.Mutex
	var notes []string
	gotNote := make(chan struct{}, 10)
	var b *Conn
	a, b := pipePair(t,
		Handlers{Call: func(ctx context.Context, op uint16, args []any) ([]any, error) {
			if op == 2 {
				return nil, errors.New("boom")
			}
			return []any{ArgString(args, 0) + "!"}, nil
		}},
		Handlers{
			Call: func(ctx context.Context, op uint16, args []any) ([]any, error) {
				// Calls back into the peer while handling a call.
				res, err := b.Call(ctx, 1, "nested")
				if err != nil {
					return nil, err
				}
				return []any{ArgInt(args, 0) * 2, ArgString(res, 0)}, nil
			},
			Notify: func(op uint16, args []any) {
				mu.Lock()
				notes = append(notes, ArgString(args, 0))
				mu.Unlock()
				gotNote <- struct{}{}
			},
		})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := a.Call(ctx, 5, 21)
	if err != nil {
		t.Fatal(err)
	}
	if ArgInt(res, 0) != 42 || ArgString(res, 1) != "nested!" {
		t.Fatalf("got %v", res)
	}
	if _, err := b.Call(ctx, 2); err == nil {
		t.Fatal("expected remote error")
	} else if re := (*RemoteError)(nil); !errors.As(err, &re) || re.Msg != "boom" {
		t.Fatalf("got %v", err)
	}

	for _, s := range []string{"one", "two", "three"} {
		if err := a.Notify(3, s); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		<-gotNote
	}
	mu.Lock()
	if !reflect.DeepEqual(notes, []string{"one", "two", "three"}) {
		t.Fatalf("notify order: %v", notes)
	}
	mu.Unlock()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := a.Call(ctx, 5, i)
			if err != nil || ArgInt(res, 0) != int64(2*i) {
				t.Errorf("call %d: %v %v", i, res, err)
			}
		}()
	}
	wg.Wait()
}

func TestConnCloseFailsPending(t *testing.T) {
	block := make(chan struct{})
	a, b := pipePair(t, Handlers{}, Handlers{Call: func(ctx context.Context, op uint16, args []any) ([]any, error) {
		<-block
		return nil, nil
	}})
	defer close(block)
	errc := make(chan error, 1)
	go func() {
		_, err := a.Call(context.Background(), 1)
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	b.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending call not failed on close")
	}
	if _, err := a.Call(context.Background(), 1); err == nil {
		t.Fatal("expected error after close")
	}
}

func TestConnCallTimeout(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	a, _ := pipePair(t, Handlers{}, Handlers{Call: func(ctx context.Context, op uint16, args []any) ([]any, error) {
		<-block
		return nil, nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := a.Call(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func BenchmarkFrameEncode(b *testing.B) {
	u := UUID{1, 2, 3}
	args := []any{u, `{"text":"hello world"}`}
	var buf []byte
	b.ReportAllocs()
	for b.Loop() {
		buf, _ = AppendFrame(buf[:0], Frame{Kind: KindNotify, Op: OpSendMessage, Args: args})
	}
}
