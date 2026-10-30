package net.minecraft.server.network;

import net.minecraft.network.Connection;
import net.minecraft.network.protocol.Packet;

public abstract class ServerCommonPacketListenerImpl {
    public final Connection connection;

    protected ServerCommonPacketListenerImpl(Connection connection) { this.connection = connection; }

    public void send(Packet<?> packet) { connection.send(packet); }

    public boolean isAcceptingMessages() { return connection.isConnected(); }
}
