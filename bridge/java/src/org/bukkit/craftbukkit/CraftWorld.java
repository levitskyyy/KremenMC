package org.bukkit.craftbukkit;

import java.lang.reflect.InvocationHandler;
import net.minecraft.server.level.ServerLevel;
import org.bukkit.World;

/** Base class of the bridge's worlds; the bridge generates the concrete subclass at runtime. */
public abstract class CraftWorld implements World {
    protected final InvocationHandler handler;

    protected CraftWorld(InvocationHandler handler) { this.handler = handler; }

    public abstract ServerLevel getHandle();
}
