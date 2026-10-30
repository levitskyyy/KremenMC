package net.minecraft.world.entity;

import java.util.concurrent.atomic.AtomicInteger;
import org.bukkit.craftbukkit.entity.CraftEntity;

public abstract class Entity {
    // Plugins take ids for client-side entities from here; start far above the ids the Go server hands out.
    private static final AtomicInteger ENTITY_COUNTER = new AtomicInteger(1 << 30);

    public abstract CraftEntity getBukkitEntity();

    public abstract int getId();

    public static int nextEntityId() { return ENTITY_COUNTER.incrementAndGet(); }
}
