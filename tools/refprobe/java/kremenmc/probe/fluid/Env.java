package kremenmc.probe.fluid;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Comparator;
import java.util.Optional;
import java.util.concurrent.Callable;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.Bootstrap;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.storage.LevelStorageSource;

// Env boots an in-process GameTestServer (flat test world) in a temp dir
// under the reference cache and runs probe code on the server thread.
final class Env {
    static PrintStream out;
    static GameTestServer server;
    static ServerLevel level;
    private static Path dir;

    static void stdout() {
        if (out == null) {
            out = System.out;
            System.setOut(System.err);
        }
    }

    static void init() {
        stdout();
        quiet();
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        quiet();
    }

    static void quiet() {
        org.apache.logging.log4j.core.config.Configurator.setRootLevel(org.apache.logging.log4j.Level.OFF);
    }

    static void boot() throws Exception {
        init();
        Path cwd = Path.of("").toAbsolutePath();
        try (var s = Files.list(cwd)) {
            s.filter(p -> p.getFileName().toString().startsWith("fluidworld-")).forEach(Env::deleteTree);
        }
        dir = Files.createTempDirectory(cwd, "fluidworld-");
        var src = LevelStorageSource.createDefault(dir);
        var access = src.createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        if (!server.isReady()) throw new IllegalStateException("server not ready");
        level = server.overworld();
        quiet();
    }

    static <T> T call(Callable<T> c) throws Exception {
        CompletableFuture<T> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                f.complete(c.call());
            } catch (Throwable t) {
                f.completeExceptionally(t);
            }
        });
        return f.get();
    }

    static void run(ThrowingRunnable r) throws Exception {
        call(() -> {
            r.run();
            return null;
        });
    }

    interface ThrowingRunnable {
        void run() throws Exception;
    }

    static final java.util.List<String> failures = new java.util.ArrayList<>();

    static void command(String cmd) {
        var src = new net.minecraft.commands.CommandSource() {
            public void sendSystemMessage(net.minecraft.network.chat.Component c) {
                failures.add(cmd + ": " + c.getString());
            }
            public boolean acceptsSuccess() { return false; }
            public boolean acceptsFailure() { return true; }
            public boolean shouldInformAdmins() { return false; }
        };
        server.getCommands().performPrefixedCommand(server.createCommandSourceStack().withSource(src), cmd);
    }

    static void waitTicks(int n) throws Exception {
        int start = call(() -> server.getTickCount());
        for (int i = 0; i < 2000 && call(() -> server.getTickCount()) < start + n; i++) Thread.sleep(10);
    }

    static void exit() {
        out.flush();
        try {
            if (server != null) {
                server.halt(false);
                for (int i = 0; i < 200 && server.isRunning(); i++) Thread.sleep(50);
                Thread.sleep(500);
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
}
