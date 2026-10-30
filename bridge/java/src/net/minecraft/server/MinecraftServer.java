package net.minecraft.server;

import net.minecraft.core.RegistryAccess;
import net.minecraft.server.network.ServerConnectionListener;

/** The Go server seen from NMS-style plugins: its connection listener and registries. */
public class MinecraftServer {
    private static MinecraftServer SERVER;

    private final ServerConnectionListener connection;

    public MinecraftServer(ServerConnectionListener connection) {
        this.connection = connection;
        SERVER = this;
    }

    public static MinecraftServer getServer() { return SERVER; }

    public ServerConnectionListener getConnection() { return connection; }

    public RegistryAccess.Frozen registryAccess() { return RegistryAccess.EMPTY; }

    public boolean isDebugging() { return false; }
}
