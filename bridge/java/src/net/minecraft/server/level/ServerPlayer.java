package net.minecraft.server.level;

import com.mojang.authlib.GameProfile;
import java.util.UUID;
import net.minecraft.network.Connection;
import net.minecraft.server.network.ServerGamePacketListenerImpl;
import net.minecraft.world.entity.player.Player;
import org.bukkit.craftbukkit.entity.CraftPlayer;

public class ServerPlayer extends Player {
    public ServerGamePacketListenerImpl connection;
    private final CraftPlayer bukkit;

    public ServerPlayer(CraftPlayer bukkit, GameProfile profile, Connection connection) {
        super(profile);
        this.bukkit = bukkit;
        this.connection = new ServerGamePacketListenerImpl(connection, this);
    }

    @Override public CraftPlayer getBukkitEntity() { return bukkit; }
    @Override public int getId() { return bukkit.getEntityId(); }
    public UUID getUUID() { return getGameProfile().id(); }
    public String getScoreboardName() { return getGameProfile().name(); }
}
