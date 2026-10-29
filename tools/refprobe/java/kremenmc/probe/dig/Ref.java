package kremenmc.probe.dig;

import java.io.IOException;
import java.nio.file.*;
import java.util.*;
import java.util.stream.Stream;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import net.minecraft.SharedConstants;
import net.minecraft.server.Bootstrap;
import net.minecraft.server.MinecraftServer;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.world.level.storage.LevelStorageSource;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.network.RegistryFriendlyByteBuf;

/** Boots an in-process reference server in a temp dir (deleted on exit). */
final class Ref {
    static final java.io.PrintStream out = new java.io.PrintStream(new java.io.BufferedOutputStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), 1 << 16), false);
    static GameTestServer server;
    static Path dir;

    static void quietLogs() throws IOException {
        Path cfg = Path.of("").toAbsolutePath().resolve("digprobe-log4j2.xml");
        Files.writeString(cfg, """
            <Configuration status="WARN"><Appenders><Console name="E" target="SYSTEM_ERR"><PatternLayout pattern="[%t/%level] %msg%n"/></Console></Appenders>
            <Loggers><Root level="warn"><AppenderRef ref="E"/></Root></Loggers></Configuration>
            """);
        System.setProperty("log4j2.configurationFile", cfg.toString());
        System.setProperty("log4j.configurationFile", cfg.toString());
    }

    static void quietLogsUnchecked() {
        try {
            quietLogs();
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }

    static GameTestServer boot() throws Exception {
        quietLogs();
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "digprobe-");
        Runtime.getRuntime().addShutdownHook(new Thread(Ref::cleanup));
        var src = LevelStorageSource.createDefault(dir);
        var access = src.createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 100000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(100);
        if (!server.isReady()) throw new IllegalStateException("server not ready");
        return server;
    }

    static void cleanup() {
        if (dir == null) return;
        try (Stream<Path> s = Files.walk(dir)) {
            s.sorted(Comparator.reverseOrder()).forEach(p -> p.toFile().delete());
        } catch (IOException ignored) {
        }
    }

    static RegistryFriendlyByteBuf buf() {
        return new RegistryFriendlyByteBuf(Unpooled.buffer(), server.registryAccess());
    }

    static RegistryFriendlyByteBuf buf(byte[] b) {
        return new RegistryFriendlyByteBuf(Unpooled.wrappedBuffer(b), server.registryAccess());
    }

    static String hex(ByteBuf b) {
        byte[] a = new byte[b.readableBytes()];
        b.getBytes(b.readerIndex(), a);
        return hex(a);
    }

    static String hex(byte[] a) {
        StringBuilder sb = new StringBuilder(a.length * 2);
        for (byte x : a) sb.append(Character.forDigit((x >> 4) & 15, 16)).append(Character.forDigit(x & 15, 16));
        return a.length == 0 ? "-" : sb.toString();
    }

    static byte[] unhex(String s) {
        if (s.equals("-")) return new byte[0];
        byte[] a = new byte[s.length() / 2];
        for (int i = 0; i < a.length; i++) a[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        return a;
    }

    static void exit() {
        out.flush();
        cleanup();
        Runtime.getRuntime().halt(0);
    }
}
