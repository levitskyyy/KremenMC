package net.minecraft.network;

import io.netty.channel.Channel;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import io.netty.util.ReferenceCountUtil;
import net.minecraft.network.protocol.Packet;

/**
 * A player's packet pipeline and its "packet_handler". Writes run through the
 * channel's handlers, so plugin handlers see every packet; the bridge consumes
 * serverbound data before it reaches this handler.
 */
public class Connection extends ChannelInboundHandlerAdapter {
    public final Channel channel;

    public Connection(Channel channel) { this.channel = channel; }

    public void send(Packet<?> packet) { channel.writeAndFlush(packet); }

    public boolean isConnected() { return channel.isOpen(); }

    public Channel channel() { return channel; }

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) { ReferenceCountUtil.release(msg); }
}
