package net.minecraft.server.network;

import io.netty.channel.ChannelFuture;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import net.minecraft.network.Connection;

/**
 * Holds the listening channel futures and the open connections. Plugins may
 * replace either list with a wrapper, so both fields stay non-final and every
 * access goes through the current field value.
 */
public class ServerConnectionListener {
    private volatile List<ChannelFuture> channels = Collections.synchronizedList(new ArrayList<>());
    private volatile List<Connection> connections = Collections.synchronizedList(new ArrayList<>());

    public void addChannel(ChannelFuture f) { channels.add(f); }

    public List<ChannelFuture> channels() { return channels; }

    public void add(Connection c) { connections.add(c); }

    public void remove(Connection c) { connections.remove(c); }

    public List<Connection> getConnections() { return connections; }
}
