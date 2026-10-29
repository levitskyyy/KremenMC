package bridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

var ErrClosed = errors.New("bridge: connection closed")

// RemoteError is an error reported by the peer in a KindError reply.
type RemoteError struct {
	Op  uint16
	Msg string
}

func (e *RemoteError) Error() string { return fmt.Sprintf("bridge: remote op %d: %s", e.Op, e.Msg) }

type Handlers struct {
	// Call handles a request; it runs on its own goroutine.
	Call func(ctx context.Context, op uint16, args []any) ([]any, error)
	// Notify handles a one-way message; it runs on the read goroutine so
	// notifications are processed in order and before any later frame.
	Notify func(op uint16, args []any)
}

// Conn is a bidirectional RPC connection speaking the protocol in protocol.go.
type Conn struct {
	nc net.Conn
	h  Handlers

	wmu  sync.Mutex
	bw   *bufio.Writer
	wbuf []byte

	nextID  atomic.Uint32
	pmu     sync.Mutex
	pending map[uint32]chan Frame

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	err    error
	done   chan struct{}
}

func NewConn(c net.Conn, h Handlers) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		nc:      c,
		h:       h,
		bw:      bufio.NewWriterSize(c, 64<<10),
		pending: make(map[uint32]chan Frame),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
}

// Serve reads frames until the connection fails or is closed.
func (c *Conn) Serve() error {
	defer close(c.done)
	br := bufio.NewReaderSize(c.nc, 64<<10)
	var buf []byte
	for {
		f, nb, err := ReadFrame(br, buf)
		buf = nb
		if err != nil {
			c.shutdown(err)
			return c.err
		}
		switch f.Kind {
		case KindReply, KindError:
			c.pmu.Lock()
			ch := c.pending[f.ID]
			delete(c.pending, f.ID)
			c.pmu.Unlock()
			if ch != nil {
				ch <- f
			}
		case KindNotify:
			if c.h.Notify != nil {
				c.safeNotify(f)
			}
		case KindCall:
			go c.handleCall(f)
		default:
			c.shutdown(fmt.Errorf("bridge: unknown frame kind %d", f.Kind))
			return c.err
		}
	}
}

func (c *Conn) safeNotify(f Frame) {
	defer func() { _ = recover() }()
	c.h.Notify(f.Op, f.Args)
}

func (c *Conn) handleCall(f Frame) {
	var res []any
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		if c.h.Call == nil {
			err = fmt.Errorf("no handler for op %d", f.Op)
			return
		}
		res, err = c.h.Call(c.ctx, f.Op, f.Args)
	}()
	reply := Frame{Kind: KindReply, ID: f.ID, Op: f.Op, Args: res}
	if err != nil {
		reply = Frame{Kind: KindError, ID: f.ID, Op: f.Op, Args: []any{err.Error()}}
	}
	if werr := c.write(reply); werr != nil && !errors.Is(werr, ErrClosed) {
		c.shutdown(werr)
	}
}

func (c *Conn) write(f Frame) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.ctx.Err() != nil {
		return ErrClosed
	}
	var err error
	c.wbuf, err = AppendFrame(c.wbuf[:0], f)
	if err != nil {
		return fmt.Errorf("bridge: encode op %d: %w", f.Op, err)
	}
	if _, err = c.bw.Write(c.wbuf); err == nil {
		err = c.bw.Flush()
	}
	if cap(c.wbuf) > 1<<20 {
		c.wbuf = nil
	}
	if err != nil {
		return fmt.Errorf("bridge: write: %w", err)
	}
	return nil
}

// Call sends a request and waits for its reply.
func (c *Conn) Call(ctx context.Context, op uint16, args ...any) ([]any, error) {
	id := c.nextID.Add(1)
	if id == 0 {
		id = c.nextID.Add(1)
	}
	ch := make(chan Frame, 1)
	c.pmu.Lock()
	if c.ctx.Err() != nil {
		c.pmu.Unlock()
		return nil, ErrClosed
	}
	c.pending[id] = ch
	c.pmu.Unlock()
	drop := func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}
	if err := c.write(Frame{Kind: KindCall, ID: id, Op: op, Args: args}); err != nil {
		drop()
		return nil, err
	}
	select {
	case f := <-ch:
		if f.Kind == KindError {
			return nil, &RemoteError{Op: op, Msg: ArgString(f.Args, 0)}
		}
		return f.Args, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, ErrClosed
	}
}

func (c *Conn) Notify(op uint16, args ...any) error {
	return c.write(Frame{Kind: KindNotify, Op: op, Args: args})
}

func (c *Conn) Close() error {
	c.shutdown(ErrClosed)
	return nil
}

// Done is closed when Serve returns.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) shutdown(err error) {
	c.once.Do(func() {
		c.pmu.Lock()
		c.err = err
		c.cancel()
		c.pmu.Unlock()
		_ = c.nc.Close()
	})
}
