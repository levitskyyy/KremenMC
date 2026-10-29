package kremenmc.bridge;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.StandardProtocolFamily;
import java.net.UnixDomainSocketAddress;
import java.nio.channels.Channels;
import java.nio.channels.SocketChannel;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Level;
import java.util.logging.Logger;

/** RPC connection to the Go server; see internal/bridge/conn.go. */
final class Conn {
    interface CallHandler {
        Object[] handle(int op, List<Object> args) throws Exception;
    }

    interface NotifyHandler {
        void handle(int op, List<Object> args);
    }

    static final class RemoteException extends RuntimeException {
        RemoteException(String msg) { super(msg); }
    }

    private static final Logger LOG = Logger.getLogger("kremenmc-bridge");
    private static final long CALL_TIMEOUT_MS = 30_000;

    private final SocketChannel channel;
    private final DataInputStream in;
    private final OutputStream out;
    private final Object writeLock = new Object();
    private final AtomicInteger nextId = new AtomicInteger();
    private final Map<Integer, CompletableFuture<Wire.Frame>> pending = new ConcurrentHashMap<>();
    private final ExecutorService callExecutor = Executors.newVirtualThreadPerTaskExecutor();
    private volatile CallHandler callHandler;
    private volatile NotifyHandler notifyHandler;
    private volatile boolean closed;

    private Conn(SocketChannel ch) {
        this.channel = ch;
        InputStream is = Channels.newInputStream(ch);
        this.in = new DataInputStream(new BufferedInputStream(is, 64 << 10));
        this.out = new BufferedOutputStream(Channels.newOutputStream(ch), 64 << 10);
    }

    /** addr is "unix:/path" or "tcp:host:port". */
    static Conn connect(String addr) throws IOException {
        SocketChannel ch;
        if (addr.startsWith("unix:")) {
            ch = SocketChannel.open(StandardProtocolFamily.UNIX);
            ch.connect(UnixDomainSocketAddress.of(addr.substring(5)));
        } else if (addr.startsWith("tcp:")) {
            String hp = addr.substring(4);
            int i = hp.lastIndexOf(':');
            ch = SocketChannel.open(new InetSocketAddress(hp.substring(0, i), Integer.parseInt(hp.substring(i + 1))));
        } else {
            throw new IOException("bad bridge address " + addr);
        }
        return new Conn(ch);
    }

    void handlers(CallHandler call, NotifyHandler notify) {
        this.callHandler = call;
        this.notifyHandler = notify;
    }

    /** Reads frames until the connection closes. */
    void serve() {
        try {
            while (true) {
                Wire.Frame f = Wire.read(in);
                switch (f.kind()) {
                    case Wire.KIND_REPLY, Wire.KIND_ERROR -> {
                        CompletableFuture<Wire.Frame> p = pending.remove(f.id());
                        if (p != null) p.complete(f);
                    }
                    case Wire.KIND_NOTIFY -> {
                        NotifyHandler h = notifyHandler;
                        if (h != null) {
                            try {
                                h.handle(f.op(), f.args());
                            } catch (Throwable t) {
                                LOG.log(Level.WARNING, "notify op " + f.op() + " failed", t);
                            }
                        }
                    }
                    case Wire.KIND_CALL -> callExecutor.execute(() -> handleCall(f));
                    default -> throw new IOException("unknown frame kind " + f.kind());
                }
            }
        } catch (EOFException e) {
            // peer closed
        } catch (IOException e) {
            if (!closed) LOG.log(Level.WARNING, "bridge connection failed", e);
        } finally {
            close();
        }
    }

    private void handleCall(Wire.Frame f) {
        byte[] reply;
        try {
            CallHandler h = callHandler;
            if (h == null) throw new IllegalStateException("no handler");
            Object[] res = h.handle(f.op(), f.args());
            reply = Wire.encode(Wire.KIND_REPLY, f.id(), f.op(), res == null ? new Object[0] : res);
        } catch (Throwable t) {
            String msg = t.getClass().getSimpleName() + ": " + t.getMessage();
            if (!(t instanceof RemoteException)) LOG.log(Level.WARNING, "call op " + f.op() + " failed", t);
            reply = Wire.encode(Wire.KIND_ERROR, f.id(), f.op(), msg);
        }
        try {
            write(reply);
        } catch (IOException e) {
            close();
        }
    }

    private void write(byte[] frame) throws IOException {
        synchronized (writeLock) {
            if (closed) throw new IOException("connection closed");
            out.write(frame);
            out.flush();
        }
    }

    List<Object> call(int op, Object... args) {
        int id = nextId.incrementAndGet();
        if (id == 0) id = nextId.incrementAndGet();
        CompletableFuture<Wire.Frame> fut = new CompletableFuture<>();
        pending.put(id, fut);
        try {
            write(Wire.encode(Wire.KIND_CALL, id, op, args));
            Wire.Frame f = fut.get(CALL_TIMEOUT_MS, TimeUnit.MILLISECONDS);
            if (f.kind() == Wire.KIND_ERROR) throw new RemoteException("kremenmc: " + Wire.str(f.args(), 0));
            return f.args();
        } catch (TimeoutException e) {
            throw new RemoteException("kremenmc: call op " + op + " timed out");
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new RemoteException("kremenmc: interrupted");
        } catch (IOException | java.util.concurrent.ExecutionException e) {
            throw new RemoteException("kremenmc: connection lost: " + e.getMessage());
        } finally {
            pending.remove(id);
        }
    }

    void notify(int op, Object... args) {
        try {
            write(Wire.encode(Wire.KIND_NOTIFY, 0, op, args));
        } catch (IOException e) {
            close();
        }
    }

    boolean isClosed() { return closed; }

    void close() {
        if (closed) return;
        closed = true;
        try {
            channel.close();
        } catch (IOException ignored) {
        }
        for (CompletableFuture<Wire.Frame> p : pending.values()) p.completeExceptionally(new IOException("closed"));
        pending.clear();
        callExecutor.shutdownNow();
    }
}
