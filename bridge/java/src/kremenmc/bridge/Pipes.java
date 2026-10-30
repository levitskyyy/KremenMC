package kremenmc.bridge;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import io.netty.channel.ChannelOutboundHandlerAdapter;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.DefaultEventLoopGroup;
import io.netty.channel.EventLoopGroup;
import io.netty.util.ReferenceCountUtil;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Level;
import net.minecraft.network.Connection;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.network.ServerConnectionListener;

/**
 * Per-player Netty channels and the listening channel plugins hook into.
 *
 * <p>When a plugin has hooked the listening channel (PacketEvents and
 * similar), Go opens a channel at join, replays the connection's handshake,
 * login and configuration packets through it with output muted so the
 * plugin's handlers track the protocol state, and from then on routes the
 * player's packet bodies through the pipeline in both directions. Without
 * such a plugin a channel is only created on demand and Go's packet path is
 * untouched.
 */
final class Pipes implements PlayerChannel.Sink {
    private static final Set<String> VANILLA = Set.of("splitter", "decoder", "prepender", "encoder", "packet_handler");
    private static final long OPEN_TIMEOUT_MS = 10_000;

    record Entry(PlayerChannel channel, Connection connection, Nms.Out out) {}

    private final Bridge b;
    private final EventLoopGroup loops;
    private final ServerConnectionListener listener = new ServerConnectionListener();
    private final PlayerChannel.Server serverChannel;
    private final InetSocketAddress localAddress;
    private final Map<UUID, Entry> byId = new ConcurrentHashMap<>();
    private volatile boolean active;

    Pipes(Bridge b) {
        this.b = b;
        int port = (int) b.infoLong("port", 25565);
        AtomicInteger n = new AtomicInteger();
        ThreadFactory tf = r -> Thread.ofPlatform().daemon().name("kremenmc-netty-" + n.incrementAndGet()).unstarted(r);
        this.loops = new DefaultEventLoopGroup(2, tf);
        this.localAddress = new InetSocketAddress(InetAddress.getLoopbackAddress(), port);
        new MinecraftServer(listener);
        serverChannel = new PlayerChannel.Server(new InetSocketAddress(port));
        loops.register(serverChannel).syncUninterruptibly();
        listener.addChannel(serverChannel.newSucceededFuture());
    }

    /** Tells Go whether a plugin hooked the listening channel; cheap enough to call every second. */
    void checkActive() {
        boolean now = serverChannel.pipeline().first() != null;
        if (now != active) {
            active = now;
            b.conn.notify(Wire.OP_PIPELINE, now);
        }
    }

    Entry get(UUID id) { return byId.get(id); }

    /** The player's channel, created without plugin hooks if Go did not open one at join. */
    Entry entry(UUID id, String address) {
        return byId.computeIfAbsent(id, k -> {
            Entry e = create(k, address);
            loops.next().register(e.channel()).syncUninterruptibly();
            listener.add(e.connection());
            return e;
        });
    }

    /**
     * Creates the player's channel the way a new connection is accepted,
     * replays history (pairs of clientbound flag and packet body) and
     * reports whether a plugin handler now sits on the byte-level part of
     * the pipeline, in which case Go routes the player's packets here.
     */
    boolean open(UUID id, String address, List<Object> history) throws Exception {
        close(id);
        Entry e = create(id, address);
        PlayerChannel ch = e.channel();
        serverChannel.eventLoop().submit(() -> serverChannel.pipeline().fireChannelRead(ch)).get(OPEN_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        loops.next().register(ch).get(OPEN_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        if (!ch.isOpen()) return false;
        listener.add(e.connection());
        byId.put(id, e);
        return ch.eventLoop().submit(() -> {
            ch.muted = true;
            try {
                for (Object o : history) {
                    List<Object> rec = Wire.list(o);
                    if (rec.size() < 2 || !(rec.get(1) instanceof byte[] body)) continue;
                    ByteBuf buf = ch.alloc().buffer(body.length).writeBytes(body);
                    if (Boolean.TRUE.equals(rec.get(0))) ch.writeAndFlush(buf, ch.voidPromise());
                    else ch.pipeline().fireChannelRead(buf);
                }
            } catch (RuntimeException ex) {
                b.log.log(Level.WARNING, "kremenmc bridge: replaying the connection history failed", ex);
            } finally {
                ch.muted = false;
            }
            return hooked(ch.pipeline());
        }).get(OPEN_TIMEOUT_MS, TimeUnit.MILLISECONDS);
    }

    private static boolean hooked(ChannelPipeline p) {
        for (Map.Entry<String, ChannelHandler> h : p) {
            if (!VANILLA.contains(h.getKey())) return true;
        }
        return false;
    }

    private Entry create(UUID id, String address) {
        PlayerChannel ch = new PlayerChannel(serverChannel, id, this, localAddress, remote(address));
        Connection conn = new Connection(ch);
        Nms.Out out = new Nms.Out(b, id);
        ChannelPipeline p = ch.pipeline();
        p.addLast("splitter", new ChannelInboundHandlerAdapter());
        p.addLast("decoder", new ChannelInboundHandlerAdapter() {
            @Override
            public void channelRead(ChannelHandlerContext ctx, Object msg) {
                if (msg instanceof ByteBuf buf) ch.receive(buf);
                else ReferenceCountUtil.release(msg);
            }
        });
        p.addLast("prepender", new ChannelOutboundHandlerAdapter());
        p.addLast("encoder", out);
        p.addLast("packet_handler", conn);
        Entry e = new Entry(ch, conn, out);
        ch.closeFuture().addListener(f -> {
            listener.remove(conn);
            byId.remove(id, e);
        });
        return e;
    }

    private static InetSocketAddress remote(String address) {
        int i = address.lastIndexOf(':');
        try {
            String host = address.substring(0, Math.max(i, 0)).replace("[", "").replace("]", "");
            return new InetSocketAddress(InetAddress.ofLiteral(host), Integer.parseInt(address.substring(i + 1)));
        } catch (RuntimeException e) {
            return new InetSocketAddress(InetAddress.getLoopbackAddress(), 0);
        }
    }

    void close(UUID id) {
        Entry e = byId.remove(id);
        if (e != null) e.channel().close();
    }

    /** A clientbound body from Go, or a marker when body is null. */
    void out(UUID id, byte[] body, long marker) {
        Entry e = byId.get(id);
        if (e == null || !e.channel().isOpen()) {
            send(id, body, marker);
            return;
        }
        PlayerChannel ch = e.channel();
        Object msg = body == null ? new PlayerChannel.Marker(marker) : ch.alloc().buffer(body.length).writeBytes(body);
        ch.writeAndFlush(msg, ch.voidPromise());
    }

    /** A serverbound body from Go. */
    void in(UUID id, byte[] body) {
        Entry e = byId.get(id);
        if (e == null || !e.channel().isOpen()) {
            receive(id, body);
            return;
        }
        PlayerChannel ch = e.channel();
        ByteBuf buf = ch.alloc().buffer(body.length).writeBytes(body);
        ch.eventLoop().execute(() -> ch.pipeline().fireChannelRead(buf));
    }

    @Override
    public void send(UUID id, byte[] body, long marker) {
        b.conn.notifyBuffered(Wire.OP_PIPE_SEND, id, body, marker);
    }

    @Override
    public void receive(UUID id, byte[] body) {
        b.conn.notifyBuffered(Wire.OP_PIPE_RECV, id, body);
    }

    void shutdown() {
        for (UUID id : byId.keySet()) close(id);
        loops.shutdownGracefully(0, 1, TimeUnit.SECONDS);
    }
}
