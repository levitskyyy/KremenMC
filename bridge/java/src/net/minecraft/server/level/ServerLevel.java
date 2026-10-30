package net.minecraft.server.level;

import net.minecraft.world.entity.Entity;
import net.minecraft.world.level.Level;
import org.bukkit.craftbukkit.CraftWorld;

public class ServerLevel extends Level {
    private final CraftWorld world;

    public ServerLevel(CraftWorld world) { this.world = world; }

    public CraftWorld getWorld() { return world; }

    public int getNextEntityId() { return Entity.nextEntityId(); }
}
