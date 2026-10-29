package kremenmc.probe.carver;

import java.lang.reflect.Field;
import java.util.Arrays;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.world.level.chunk.status.ChunkStatus;

// Live boots the official dedicated server in-process and requests chunks
// at TERRAIN in a dimension between BEGIN/END hook events, so Jdi observes
// only that generation. Args: <dimension> <cx,cz>... -- <server args>
public final class Live {
    public static void main(String[] args) throws Exception {
        int sep = Arrays.asList(args).indexOf("--");
        String dim = args[0];
        net.minecraft.server.Main.main(Arrays.copyOfRange(args, sep + 1, args.length));
        MinecraftServer server = awaitServer();
        int code = 0;
        try {
            ServerLevel level = server.getLevel(ResourceKey.create(Registries.DIMENSION, Identifier.parse(dim)));
            var cache = level.getChunkSource();
            Hook.ev("BEGIN");
            for (int i = 1; i < sep; i++) {
                String[] p = args[i].split(",");
                int x = Integer.parseInt(p[0]), z = Integer.parseInt(p[1]);
                Hook.ev("chunk " + x + " " + z);
                var f = java.util.concurrent.CompletableFuture.supplyAsync(() -> cache.getChunkFuture(x, z, ChunkStatus.TERRAIN, true), server).thenCompose(r -> r);
                f.get(30, TimeUnit.MINUTES);
            }
            Hook.ev("END");
            System.out.println("kremenmc-oracle: ok");
        } catch (Throwable t) {
            System.out.println("kremenmc-oracle: failed: " + t);
            t.printStackTrace();
            code = 1;
        }
        System.out.flush();
        server.halt(true);
        System.exit(code);
    }

    private static MinecraftServer awaitServer() throws Exception {
        MinecraftServer server = null;
        for (int i = 0; i < 600 && server == null; i++) {
            server = fromShutdownHooks();
            if (server == null) Thread.sleep(100);
        }
        if (server == null) throw new IllegalStateException("server did not start");
        long deadline = System.currentTimeMillis() + 15 * 60_000;
        while (!server.isReady()) {
            if (server.isStopped() || System.currentTimeMillis() > deadline) throw new IllegalStateException("server failed to start");
            Thread.sleep(100);
        }
        return server;
    }

    @SuppressWarnings("unchecked")
    private static MinecraftServer fromShutdownHooks() throws Exception {
        Class<?> hooksClass = Class.forName("java.lang.ApplicationShutdownHooks");
        Field f = hooksClass.getDeclaredField("hooks");
        f.setAccessible(true);
        synchronized (hooksClass) {
            Map<Thread, Thread> hooks = (Map<Thread, Thread>) f.get(null);
            if (hooks == null) return null;
            for (Thread t : hooks.keySet()) {
                if (!t.getClass().getName().equals("net.minecraft.server.Main$1")) continue;
                Field s = t.getClass().getDeclaredField("val$dedicatedServer");
                s.setAccessible(true);
                return (MinecraftServer) s.get(t);
            }
        }
        return null;
    }
}
