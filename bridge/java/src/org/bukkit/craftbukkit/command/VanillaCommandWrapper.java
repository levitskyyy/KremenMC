package org.bukkit.craftbukkit.command;

import io.papermc.paper.command.brigadier.CommandSourceStack;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Proxy;
import org.bukkit.Location;
import org.bukkit.command.CommandSender;
import org.bukkit.entity.Entity;

/** Maps a Bukkit sender to a brigadier command source, for command frameworks that look it up reflectively. */
public final class VanillaCommandWrapper {
    private VanillaCommandWrapper() {}

    public static CommandSourceStack getListener(CommandSender sender) {
        Entity entity = sender instanceof Entity e ? e : null;
        InvocationHandler h = (proxy, m, args) -> switch (m.getName()) {
            case "getSender" -> sender;
            case "getExecutor" -> entity;
            case "getLocation" -> entity != null ? entity.getLocation() : new Location(null, 0, 0, 0);
            case "withLocation", "withExecutor" -> proxy;
            case "equals" -> proxy == args[0];
            case "hashCode" -> System.identityHashCode(proxy);
            case "toString" -> "CommandSourceStack{" + sender.getName() + "}";
            default -> m.isDefault() ? InvocationHandler.invokeDefault(proxy, m, args) : null;
        };
        return (CommandSourceStack) Proxy.newProxyInstance(VanillaCommandWrapper.class.getClassLoader(), new Class<?>[] {CommandSourceStack.class}, h);
    }
}
