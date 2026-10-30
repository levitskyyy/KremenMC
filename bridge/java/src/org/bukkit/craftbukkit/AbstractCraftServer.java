package org.bukkit.craftbukkit;

import java.lang.reflect.InvocationHandler;
import org.bukkit.Server;

/**
 * Base class of the bridge's Server. The bridge generates the concrete
 * subclass org.bukkit.craftbukkit.CraftServer at runtime, so plugins that
 * derive the CraftBukkit package from Bukkit.getServer().getClass() find the
 * classes in this package.
 */
public abstract class AbstractCraftServer implements Server {
    protected final InvocationHandler handler;

    protected AbstractCraftServer(InvocationHandler handler) { this.handler = handler; }
}
