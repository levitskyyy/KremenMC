package kremenmc.bridge;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.channel.AbstractChannel;
import io.netty.channel.AbstractServerChannel;
import io.netty.channel.Channel;
import io.netty.channel.ChannelConfig;
import io.netty.channel.ChannelMetadata;
import io.netty.channel.ChannelOutboundBuffer;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultChannelConfig;
import io.netty.channel.EventLoop;
import io.netty.channel.SingleThreadEventLoop;
import io.netty.util.ReferenceCountUtil;
import java.net.SocketAddress;
import java.util.UUID;

/**
 * A player's connection as a Netty channel. The Go server owns the socket:
 * whatever reaches the head of the pipeline (packet bodies, after plugin
 * handlers ran) goes back to Go to be sent, and serverbound bodies from Go
 * enter at the head. Each channel runs on one event loop thread, like a
 * vanilla connection.
 */
final class PlayerChannel extends AbstractChannel {
    /** Where the channel's traffic leaves the JVM. */
    interface Sink {
        void send(UUID id, byte[] body, long marker);

        void receive(UUID id, byte[] body);
    }

    /** Passes through the pipeline in order with packets; Go applies a deferred action when it comes back. */
    record Marker(long id) {}

    private static final ChannelMetadata METADATA = new ChannelMetadata(false);

    final UUID id;
    private final Sink sink;
    private final ChannelConfig config = new DefaultChannelConfig(this);
    private final SocketAddress local;
    private final SocketAddress remote;
    private volatile boolean open = true;
    /** True while history is replayed: plugins observe it, nothing leaves the JVM. */
    boolean muted;

    PlayerChannel(Channel parent, UUID id, Sink sink, SocketAddress local, SocketAddress remote) {
        super(parent);
        this.id = id;
        this.sink = sink;
        this.local = local;
        this.remote = remote;
    }

    void receive(ByteBuf body) {
        if (muted) {
            ReferenceCountUtil.release(body);
            return;
        }
        try {
            if (body.isReadable()) sink.receive(id, ByteBufUtil.getBytes(body));
        } finally {
            ReferenceCountUtil.release(body);
        }
    }

    @Override
    protected void doWrite(ChannelOutboundBuffer in) {
        for (Object msg; (msg = in.current()) != null; ) {
            if (!muted) {
                if (msg instanceof ByteBuf buf) {
                    if (buf.isReadable()) sink.send(id, ByteBufUtil.getBytes(buf), 0);
                } else if (msg instanceof Marker m) {
                    sink.send(id, null, m.id());
                }
            }
            in.remove();
        }
    }

    @Override protected AbstractUnsafe newUnsafe() { return new Unsafe(); }
    @Override protected boolean isCompatible(EventLoop loop) { return loop instanceof SingleThreadEventLoop; }
    @Override protected SocketAddress localAddress0() { return local; }
    @Override protected SocketAddress remoteAddress0() { return remote; }
    @Override protected void doBind(SocketAddress a) {}
    @Override protected void doDisconnect() { open = false; }
    @Override protected void doClose() { open = false; }
    @Override protected void doBeginRead() {}
    @Override public ChannelConfig config() { return config; }
    @Override public boolean isOpen() { return open; }
    @Override public boolean isActive() { return open; }
    @Override public ChannelMetadata metadata() { return METADATA; }

    private final class Unsafe extends AbstractUnsafe {
        @Override
        public void connect(SocketAddress remote, SocketAddress local, ChannelPromise p) {
            p.setFailure(new UnsupportedOperationException("kremenmc bridge: player channels are already connected"));
        }
    }

    /** The listening channel plugins hook to see new connections. */
    static final class Server extends AbstractServerChannel {
        private final ChannelConfig config = new DefaultChannelConfig(this);
        private final SocketAddress local;
        private volatile boolean open = true;

        Server(SocketAddress local) { this.local = local; }

        @Override protected boolean isCompatible(EventLoop loop) { return loop instanceof SingleThreadEventLoop; }
        @Override protected SocketAddress localAddress0() { return local; }
        @Override protected void doBind(SocketAddress a) {}
        @Override protected void doClose() { open = false; }
        @Override protected void doBeginRead() {}
        @Override public ChannelConfig config() { return config; }
        @Override public boolean isOpen() { return open; }
        @Override public boolean isActive() { return open; }
    }
}
