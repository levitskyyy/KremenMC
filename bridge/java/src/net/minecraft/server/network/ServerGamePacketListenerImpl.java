package net.minecraft.server.network;

import net.minecraft.network.Connection;
import net.minecraft.server.level.ServerPlayer;

public class ServerGamePacketListenerImpl extends ServerCommonPacketListenerImpl {
    public final ServerPlayer player;

    public ServerGamePacketListenerImpl(Connection connection, ServerPlayer player) {
        super(connection);
        this.player = player;
    }

    public ServerPlayer getPlayer() { return player; }
}
