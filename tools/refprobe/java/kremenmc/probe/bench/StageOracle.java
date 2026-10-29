package kremenmc.probe.bench;

import java.io.BufferedOutputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.OutputStream;
import java.lang.reflect.Field;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.zip.GZIPOutputStream;
import net.minecraft.core.registries.Registries;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.NbtIo;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerChunkCache;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.world.level.chunk.ChunkAccess;
import net.minecraft.world.level.chunk.ImposterProtoChunk;
import net.minecraft.world.level.chunk.status.ChunkStatus;
import net.minecraft.world.level.chunk.storage.SerializableChunkData;

/**
 * Stage oracle: boots the official dedicated server in-process (no players),
 * then asks its own chunk system for each requested chunk at a given status
 * (ServerChunkCache.getChunkFuture) and dumps the resulting proto-chunk as
 * vanilla chunk NBT (SerializableChunkData). Up to TERRAIN a chunk's content
 * depends only on its own position plus neighbours' biomes and structure
 * starts, so the dumps are independent of request order.
 *
 * Usage: StageOracle <config> <server args...>; config lines:
 *   dim <dimension id>
 *   stage <name> <chunk status> <output file>
 *   chunk <x> <z>
 *   heightmaps <output file>   (vanilla heightmap predicates of every block state)
 * Each output file is a gzip stream of records: int32 length + named-root NBT.
 */
public final class StageOracle {
    private static final long STARTUP_TIMEOUT_MS = 15 * 60_000;

    private record Stage(String name, String status, Path out) {}

    public static void main(String[] args) throws Exception {
        String dim = null;
        Path heightmaps = null;
        List<Stage> stages = new ArrayList<>();
        List<int[]> chunks = new ArrayList<>();
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            String[] f = line.trim().split("\\s+");
            switch (f[0]) {
                case "dim" -> dim = f[1];
                case "stage" -> stages.add(new Stage(f[1], f[2], Path.of(f[3])));
                case "chunk" -> chunks.add(new int[] {Integer.parseInt(f[1]), Integer.parseInt(f[2])});
                case "heightmaps" -> heightmaps = Path.of(f[1]);
                default -> {}
            }
        }
        net.minecraft.server.Main.main(Arrays.copyOfRange(args, 1, args.length));
        MinecraftServer server = awaitServer();
        int code = 0;
        try {
            ServerLevel level = server.getLevel(ResourceKey.create(Registries.DIMENSION, Identifier.parse(dim)));
            if (level == null) throw new IllegalArgumentException("no dimension " + dim + "; have " + server.levelKeys());
            for (Stage s : stages) dump(server, level, s, chunks);
            if (heightmaps != null) Files.writeString(heightmaps, server.submit(HeightmapPredicates::dump).get());
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

    private static void dump(MinecraftServer server, ServerLevel level, Stage s, List<int[]> chunks) throws Exception {
        ChunkStatus status = ChunkStatus.byName(s.status());
        ServerChunkCache cache = level.getChunkSource();
        long t0 = System.nanoTime();
        AtomicInteger done = new AtomicInteger();
        List<CompletableFuture<CompoundTag>> futures = new ArrayList<>();
        for (int[] c : chunks) {
            int x = c[0], z = c[1];
            CompletableFuture<CompoundTag> f = CompletableFuture
                .supplyAsync(() -> cache.getChunkFuture(x, z, status, true), server)
                .thenCompose(r -> r)
                .thenApplyAsync(r -> {
                    ChunkAccess chunk = r.orElse(null);
                    if (chunk == null) throw new IllegalStateException("chunk " + x + "," + z + ": " + r.getError());
                    // Already generated further (e.g. by the spawn search): dump what exists; the Go side flags the status.
                    if (chunk instanceof ImposterProtoChunk imp) chunk = imp.getWrapped();
                    SerializableChunkData data = SerializableChunkData.copyOf(level, chunk);
                    CompoundTag tag = data.write();
                    tag.putString("kremenmc_stage", s.name());
                    tag.putString("kremenmc_requested_status", status.getName());
                    tag.putInt("kremenmc_done_order", done.getAndIncrement());
                    tag.putLong("kremenmc_done_us", (System.nanoTime() - t0) / 1000);
                    return tag;
                }, server);
            futures.add(f);
        }
        CompletableFuture.allOf(futures.toArray(CompletableFuture[]::new)).get(30, TimeUnit.MINUTES);
        Files.createDirectories(s.out().toAbsolutePath().getParent());
        try (OutputStream fo = Files.newOutputStream(s.out());
             DataOutputStream out = new DataOutputStream(new BufferedOutputStream(new GZIPOutputStream(fo, 1 << 16)))) {
            ByteArrayOutputStream rec = new ByteArrayOutputStream(64 << 10);
            for (CompletableFuture<CompoundTag> f : futures) {
                rec.reset();
                NbtIo.write(f.get(), new DataOutputStream(rec));
                out.writeInt(rec.size());
                rec.writeTo(out);
            }
        }
        System.out.printf("kremenmc-oracle: stage %s (%s): %d chunks in %.1fs%n", s.name(), s.status(), chunks.size(), (System.nanoTime() - t0) / 1e9);
    }

    /** Finds the server Main started through the shutdown hook it registers, then waits until it is ready. */
    private static MinecraftServer awaitServer() throws Exception {
        MinecraftServer server = fromShutdownHooks();
        if (server == null) fatal("dedicated server did not start");
        long deadline = System.currentTimeMillis() + STARTUP_TIMEOUT_MS;
        while (!server.isReady()) {
            if (server.isStopped() || System.currentTimeMillis() > deadline) fatal("dedicated server failed to start");
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

    private static void fatal(String msg) {
        System.err.println(msg);
        System.exit(1);
    }
}
