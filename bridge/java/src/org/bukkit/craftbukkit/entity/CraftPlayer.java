package org.bukkit.craftbukkit.entity;

import com.mojang.authlib.GameProfile;
import java.lang.reflect.InvocationHandler;
import net.minecraft.server.level.ServerPlayer;
import org.bukkit.entity.Player;

/**
 * Base class of the bridge's online players, so NMS-style plugins can cast
 * to CraftPlayer. The bridge generates the concrete subclass at runtime;
 * every abstract method, these included, is routed to the handler.
 */
public abstract class CraftPlayer extends CraftEntity implements Player {
    protected CraftPlayer(InvocationHandler handler) { super(handler); }

    @Override
    public abstract ServerPlayer getHandle();

    public abstract GameProfile getProfile();
}
