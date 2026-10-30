package org.bukkit.craftbukkit.entity;

import java.lang.reflect.InvocationHandler;
import net.minecraft.world.entity.Entity;

/** Base class of the bridge's entities; the bridge generates concrete subclasses at runtime. */
public abstract class CraftEntity implements org.bukkit.entity.Entity {
    protected final InvocationHandler handler;

    protected CraftEntity(InvocationHandler handler) { this.handler = handler; }

    public abstract Entity getHandle();
}
