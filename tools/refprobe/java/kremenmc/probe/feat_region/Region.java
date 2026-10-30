package kremenmc.probe.feat_region;

import java.io.PrintStream;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.function.Supplier;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.GenerationChunkHolder;
import net.minecraft.server.level.ServerChunkCache;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.WorldGenRegion;
import net.minecraft.util.StaticCache2D;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.chunk.ChunkAccess;
import net.minecraft.world.level.chunk.status.ChunkPyramid;
import net.minecraft.world.level.chunk.status.ChunkStatus;
import net.minecraft.world.level.chunk.status.ChunkStep;
import net.minecraft.world.level.levelgen.Heightmap;

/**
 * Region decorates one chunk the way the features status does, in a real
 * WorldGenRegion of a seeded world whose 3x3 neighbourhood is at TERRAIN,
 * and logs every block write (and, for placed features whose key contains
 * a "keep" token, every block, fluid and heightmap read) through a proxy
 * WorldGenLevel handed to the generator's own applyBiomeDecoration.
 *
 * Config lines: "out <file>", "chunk <cx> <cz>", "keep <substring>",
 * "rrandom" (dump the region's getRandom() state and two longs first).
 * Output: "C cx cz", "F key", "S x y z state ok", "B x y z state",
 * "L x y z fluid", "H type x z value", "P type x y z -> x y z".
 */
public final class Region {
    static PrintStream out;
    static String cur = "";
    static boolean keep;
    static List<String> keeps = new ArrayList<>();
    static boolean rrandom;

    static String state(BlockState s) {
        StringBuilder b = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString());
        TreeMap<String, String> props = new TreeMap<>();
        for (Property<?> p : s.getProperties()) props.put(p.getName(), s.getValue(p).toString().toLowerCase());
        if (!props.isEmpty()) {
            b.append('[');
            String sep = "";
            for (Map.Entry<String, String> e : props.entrySet()) {
                b.append(sep).append(e.getKey()).append('=').append(e.getValue());
                sep = ",";
            }
            b.append(']');
        }
        return b.toString();
    }

    static String pos(BlockPos p) { return p.getX() + " " + p.getY() + " " + p.getZ(); }

    public static void main(String[] args) throws Exception {
        List<int[]> chunks = new ArrayList<>();
        Path outPath = null;
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            String[] f = line.trim().split("\\s+");
            switch (f[0]) {
                case "out" -> outPath = Path.of(f[1]);
                case "chunk" -> chunks.add(new int[] {Integer.parseInt(f[1]), Integer.parseInt(f[2])});
                case "keep" -> keeps.add(f[1]);
                case "rrandom" -> rrandom = true;
                default -> {}
            }
        }
        out = new PrintStream(Files.newOutputStream(outPath), false);
        net.minecraft.server.Main.main(Arrays.copyOfRange(args, 1, args.length));
        MinecraftServer server = awaitServer();
        int code = 0;
        try {
            ServerLevel level = server.overworld();
            ServerChunkCache cache = level.getChunkSource();
            for (int[] c : chunks) {
                List<CompletableFuture<?>> fs = new ArrayList<>();
                for (int dx = -1; dx <= 1; dx++)
                    for (int dz = -1; dz <= 1; dz++) {
                        int x = c[0] + dx, z = c[1] + dz;
                        fs.add(CompletableFuture.supplyAsync(() -> cache.getChunkFuture(x, z, ChunkStatus.TERRAIN, true), server).thenCompose(r -> r));
                    }
                CompletableFuture<Void> done = CompletableFuture.allOf(fs.toArray(CompletableFuture[]::new))
                    .thenRunAsync(() -> decorate(level, c[0], c[1]), server);
                done.get(10, TimeUnit.MINUTES);
            }
            System.out.println("kremenmc-oracle: ok");
        } catch (Throwable t) {
            System.out.println("kremenmc-oracle: failed: " + t);
            t.printStackTrace(System.out);
            code = 1;
        }
        out.flush();
        System.out.flush();
        server.halt(true);
        System.exit(code);
    }

    static void decorate(ServerLevel level, int cx, int cz) {
        var map = level.getChunkSource().chunkMap;
        ChunkStep step = ChunkPyramid.GENERATION_PYRAMID.getStepTo(ChunkStatus.FEATURES);
        int radius = step.getAccumulatedRadiusOf(ChunkStatus.EMPTY);
        StaticCache2D<GenerationChunkHolder> holders = StaticCache2D.create(cx, cz, radius, (x, z) -> map.getUpdatingChunkIfPresent(ChunkPos.pack(x, z)));
        GenerationChunkHolder ch = holders.get(cx, cz);
        ChunkAccess center = ch.getChunkIfPresentUnchecked(ChunkStatus.TERRAIN);
        out.println("C " + cx + " " + cz + " status=" + center.getPersistedStatus().getName());
        for (int dx = -1; dx <= 1; dx++)
            for (int dz = -1; dz <= 1; dz++) {
                var h = holders.get(cx + dx, cz + dz);
                out.println("# neighbour " + (cx + dx) + " " + (cz + dz) + " " + (h == null ? "none" : h.getPersistedStatus().getName()));
            }
        WorldGenRegion region = new WorldGenRegion(level, holders, step, center);
        WorldGenLevel proxy = (WorldGenLevel) Proxy.newProxyInstance(Region.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, new Logger(region));
        var sm = level.structureManager().forWorldGenRegion(region);
        int minY = level.getMinY(), maxY = level.getMaxY();
        int x0 = (cx - 1) << 4, z0 = (cz - 1) << 4;
        BlockState[] before = new BlockState[48 * 48 * (maxY - minY + 1)];
        BlockPos.MutableBlockPos mp = new BlockPos.MutableBlockPos();
        int i = 0;
        for (int y = minY; y <= maxY; y++)
            for (int x = 0; x < 48; x++)
                for (int z = 0; z < 48; z++) before[i++] = region.getBlockState(mp.set(x0 + x, y, z0 + z));
        if (rrandom) {
            Object g = region.getRandom();
            StringBuilder sb = new StringBuilder("G " + cx + " " + cz + " " + g.getClass().getName());
            for (Class<?> k = g.getClass(); k != null && k != Object.class; k = k.getSuperclass())
                for (Field fl : k.getDeclaredFields()) {
                    if (java.lang.reflect.Modifier.isStatic(fl.getModifiers())) continue;
                    try {
                        fl.setAccessible(true);
                        Object v = fl.get(g);
                        sb.append(' ').append(fl.getName()).append('=').append(v);
                        if (v != null && v.getClass().getName().contains("Xoroshiro")) {
                            for (Field f2 : v.getClass().getDeclaredFields()) {
                                if (java.lang.reflect.Modifier.isStatic(f2.getModifiers())) continue;
                                f2.setAccessible(true);
                                sb.append(' ').append(fl.getName()).append('.').append(f2.getName()).append('=').append(f2.get(v));
                            }
                        }
                    } catch (Throwable t) {
                        sb.append(' ').append(fl.getName()).append("=?");
                    }
                }
            var rs = (net.minecraft.util.RandomSource) g;
            sb.append(" next=").append(rs.nextLong()).append(',').append(rs.nextLong());
            out.println(sb);
        }
        level.getChunkSource().getGenerator().applyBiomeDecoration(proxy, center, sm);
        out.println("E");
        i = 0;
        for (int y = minY; y <= maxY; y++)
            for (int x = 0; x < 48; x++)
                for (int z = 0; z < 48; z++) {
                    BlockState s = region.getBlockState(mp.set(x0 + x, y, z0 + z));
                    if (s != before[i++]) out.println("D " + (x0 + x) + " " + y + " " + (z0 + z) + " " + state(s));
                }
    }

    static final class Logger implements InvocationHandler {
        final WorldGenRegion r;

        Logger(WorldGenRegion r) { this.r = r; }

        @Override
        public Object invoke(Object p, Method m, Object[] a) throws Throwable {
            String n = m.getName();
            if (n.equals("setCurrentlyGenerating")) {
                @SuppressWarnings("unchecked")
                Supplier<String> s = (Supplier<String>) a[0];
                cur = s == null ? "" : s.get();
                keep = false;
                for (String k : keeps) if (cur.contains(k)) keep = true;
                out.println("F " + cur);
            }
            Object v;
            try {
                v = m.invoke(r, a);
            } catch (InvocationTargetException e) {
                throw e.getCause();
            }
            switch (n) {
                case "setBlock" -> out.println("S " + pos((BlockPos) a[0]) + " " + state((BlockState) a[1]) + " " + v);
                case "getBlockState" -> { if (keep) out.println("B " + pos((BlockPos) a[0]) + " " + state((BlockState) v)); }
                case "getFluidState" -> { if (keep) out.println("L " + pos((BlockPos) a[0]) + " " + v); }
                case "getHeight" -> { if (keep && a != null && a.length == 3) out.println("H " + a[0] + " " + a[1] + " " + a[2] + " " + v); }
                case "getHeightmapPos" -> { if (keep) out.println("P " + a[0] + " " + pos((BlockPos) a[1]) + " -> " + pos((BlockPos) v)); }
                case "isStateAtPosition", "isFluidAtPosition" -> { if (keep) out.println("I " + n + " " + pos((BlockPos) a[0]) + " " + v + " " + state(r.getBlockState((BlockPos) a[0]))); }
                default -> {}
            }
            return v;
        }
    }

    private static MinecraftServer awaitServer() throws Exception {
        MinecraftServer server = fromShutdownHooks();
        if (server == null) throw new IllegalStateException("dedicated server did not start");
        long deadline = System.currentTimeMillis() + 15 * 60_000;
        while (!server.isReady()) {
            if (server.isStopped() || System.currentTimeMillis() > deadline) throw new IllegalStateException("dedicated server failed to start");
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
