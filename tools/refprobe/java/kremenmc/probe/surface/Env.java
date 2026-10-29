package kremenmc.probe.surface;

import java.io.PrintStream;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Comparator;
import java.util.Optional;
import net.minecraft.SharedConstants;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.Bootstrap;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.storage.LevelStorageSource;

// Env boots an in-process GameTestServer in a temp dir (for a full
// RegistryAccess and a ServerLevel to serialize chunks with) and offers
// reflective helpers for observing private state.
final class Env {
    static PrintStream out;
    static MinecraftServer server;
    static ServerLevel level;
    private static Path dir;

    static void quiet() {
        org.apache.logging.log4j.core.config.Configurator.setRootLevel(org.apache.logging.log4j.Level.OFF);
    }

    static void boot() throws Exception {
        out = System.out;
        System.setOut(System.err);
        quiet();
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        quiet();
        Path cwd = Path.of("").toAbsolutePath();
        try (var s = Files.list(cwd)) {
            s.filter(p -> p.getFileName().toString().startsWith("surfaceworld-")).forEach(Env::deleteTree);
        }
        dir = Files.createTempDirectory(cwd, "surfaceworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 2400 && !server.isReady(); i++) Thread.sleep(50);
        if (!server.isReady()) throw new IllegalStateException("server not ready");
        level = server.overworld();
        quiet();
    }

    static void exit() {
        out.flush();
        try {
            if (server != null) {
                server.halt(false);
                for (int i = 0; i < 200 && server.isRunning(); i++) Thread.sleep(50);
            }
        } catch (InterruptedException ignored) {
        }
        if (dir != null) deleteTree(dir);
        out.flush();
        Runtime.getRuntime().halt(0);
    }

    static void deleteTree(Path p) {
        try (var w = Files.walk(p)) {
            w.sorted(Comparator.reverseOrder()).forEach(q -> q.toFile().delete());
        } catch (Exception ignored) {
        }
    }

    static Object get(Object o, Class<?> c, String name) throws ReflectiveOperationException {
        Field f = c.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(o);
    }

    static Method method(Class<?> c, String name) {
        for (Method m : c.getDeclaredMethods()) {
            if (m.getName().equals(name)) {
                m.setAccessible(true);
                return m;
            }
        }
        throw new IllegalArgumentException("no method " + name + " in " + c);
    }
}
