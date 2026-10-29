package kremenmc.probe.feat_disk;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.RegistryOps;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.storage.LevelStorageSource;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.levelgen.Heightmap;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;

// Place places a feature (registry key or inline JSON) at (x, y0, z) in a
// hashed scene with a seeded xoroshiro WorldgenRandom and prints every block
// of the box that differs from the scene plus the random's next long
// (FEAT_LOG=1 also logs every draw). Input lines:
//   <mode>[:<sea level>] <y0> <seed> <x> <z> <feature>
// Modes: air, flat (air above a hashed ground block at y0-1 with dirt
// below and stone obstacles in the air; flatlow: one lower, flatwater: two
// layers of water on the ground, flatx: other ground blocks, flatorigin:
// a plant, snow or water at the origin, flatcombo: hashed origin and the
// block above), ice (water to y0+8 with hashed
// packed ice, ice, snow, stone and air cells), sea (water to y0 over a
// stone floor 11 to 14 below), mix (every cell hashed from a 16-block palette), ground (columns:
// stone, dirt, a hashed top block at a hashed height, water or air above).
// Mirrored by feature/disk_test.go.
public final class Place {
    static final int R = 16;
    static Block[] MIX, TOP, FLAT, FLATX, ORIGIN, COMBO0, COMBO1;
    static void init() {
        MIX = new Block[]{Blocks.DIRT, Blocks.GRASS_BLOCK, Blocks.CLAY, Blocks.MUD, Blocks.AIR, Blocks.WATER, Blocks.STONE, Blocks.SAND,
        Blocks.SNOW_BLOCK, Blocks.ICE, Blocks.PODZOL, Blocks.COARSE_DIRT, Blocks.MYCELIUM, Blocks.GRAVEL, Blocks.AIR, Blocks.DIRT};
        TOP = new Block[]{Blocks.GRASS_BLOCK, Blocks.GRASS_BLOCK, Blocks.DIRT, Blocks.MUD, Blocks.CLAY, Blocks.SAND, Blocks.SNOW_BLOCK, Blocks.GRAVEL};
        FLAT = new Block[]{Blocks.GRASS_BLOCK, Blocks.GRASS_BLOCK, Blocks.DIRT, Blocks.PODZOL, Blocks.COARSE_DIRT, Blocks.SAND, Blocks.MUD, Blocks.STONE};
        FLATX = new Block[]{Blocks.MYCELIUM, Blocks.MOSS_BLOCK, Blocks.ROOTED_DIRT, Blocks.GRAVEL, Blocks.SNOW_BLOCK, Blocks.PALE_MOSS_BLOCK, Blocks.MUDDY_MANGROVE_ROOTS, Blocks.RED_SAND};
        ORIGIN = new Block[]{Blocks.SHORT_GRASS, Blocks.SNOW, Blocks.DEAD_BUSH, Blocks.WATER};
        COMBO0 = new Block[]{Blocks.AIR, Blocks.WATER, Blocks.SHORT_GRASS, Blocks.GRASS_BLOCK, Blocks.STONE, Blocks.OAK_LEAVES, Blocks.VINE, Blocks.LAVA};
        COMBO1 = new Block[]{Blocks.AIR, Blocks.WATER, Blocks.SHORT_GRASS, Blocks.STONE};
    }

    static String state(BlockState s) {
        StringBuilder b = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath());
        String sep = "[";
        for (Property<?> p : s.getProperties()) {
            b.append(sep).append(p.getName()).append('=').append(s.getValue(p).toString().toLowerCase());
            sep = ",";
        }
        if (!sep.equals("[")) b.append(']');
        return b.toString();
    }

    static int hash(long seed, int dx, int dy, int dz) {
        int h = dx * 73856093 ^ dy * 19349663 ^ dz * 83492791 ^ (int) seed;
        h ^= h >>> 13; h *= 0x5bd1e995; h ^= h >>> 15;
        return h;
    }

    static BlockState scene(String mode, long seed, int dx, int dy, int dz) {
        if (mode.equals("air")) return Blocks.AIR.defaultBlockState();
        if (mode.startsWith("flat")) {
            if (mode.equals("flatlow")) dy--;
            int h = hash(seed, dx, 0, dz);
            if (mode.equals("flatwater") && dy >= 0 && dy <= 1) return Blocks.WATER.defaultBlockState();
            if (mode.equals("flatorigin") && dx == 0 && dz == 0 && dy == 0) return ORIGIN[(int) (seed >> 9) & 3].defaultBlockState();
            if (mode.equals("flatcombo") && dx == 0 && dz == 0 && dy == 0) return COMBO0[(int) (seed >> 9) & 7].defaultBlockState();
            if (mode.equals("flatcombo") && dx == 0 && dz == 0 && dy == 1) return COMBO1[(int) (seed >> 12) & 3].defaultBlockState();
            if (dy >= 0) {
                if (dx == 0 && dz == 0 && (seed & 3) == 0 && dy == 2 + ((seed >> 2) & 15)) return Blocks.STONE.defaultBlockState();
                if (!(dx == 0 && dz == 0) && ((h >>> 16) & 15) == 0 && dy == ((h >>> 20) & 15)) return Blocks.STONE.defaultBlockState();
                return Blocks.AIR.defaultBlockState();
            }
            Block[] g = mode.equals("flatx") ? FLATX : FLAT;
            if (dy == -1) return g[(dx == 0 && dz == 0 ? (int) (seed >> 6) : h >>> 8) & 7].defaultBlockState();
            if (dy >= -4) return ((hash(seed, dx, dy, dz) >>> 4) & 15) == 0 ? Blocks.AIR.defaultBlockState() : Blocks.DIRT.defaultBlockState();
            return Blocks.STONE.defaultBlockState();
        }
        if (mode.equals("ice")) {
            if (dy > 8) return Blocks.AIR.defaultBlockState();
            if (dy < -8) return Blocks.STONE.defaultBlockState();
            return switch ((hash(seed, dx, dy, dz) >>> 8) & 15) {
                case 0, 1, 2 -> Blocks.PACKED_ICE.defaultBlockState();
                case 3 -> Blocks.AIR.defaultBlockState();
                case 4 -> Blocks.ICE.defaultBlockState();
                case 5 -> Blocks.SNOW_BLOCK.defaultBlockState();
                case 6 -> Blocks.STONE.defaultBlockState();
                default -> Blocks.WATER.defaultBlockState();
            };
        }
        if (mode.equals("sea")) {
            if (dy > 0) return Blocks.AIR.defaultBlockState();
            int floor = -14 + ((hash(seed, dx, 0, dz) >>> 4) & 3);
            return dy <= floor ? Blocks.STONE.defaultBlockState() : Blocks.WATER.defaultBlockState();
        }
        if (mode.equals("mix")) return MIX[(hash(seed, dx, dy, dz) >>> 8) & 15].defaultBlockState();
        int h = hash(seed, dx, 0, dz);
        int top = ((h >>> 4) & 7) - 4;
        boolean wet = ((h >>> 12) & 3) != 0;
        if (dy > top) return wet && dy <= 1 ? Blocks.WATER.defaultBlockState() : Blocks.AIR.defaultBlockState();
        if (dy == top) return TOP[(h >>> 8) & 7].defaultBlockState();
        if (((hash(seed, dx, dy, dz) >>> 4) & 15) == 0) return Blocks.AIR.defaultBlockState();
        if (dy >= top - 3) return Blocks.DIRT.defaultBlockState();
        return Blocks.STONE.defaultBlockState();
    }

    // Wrap hands the feature the ServerLevel as a WorldGenLevel. Heightmaps
    // are recomputed by scanning down from the top of the scene box (the
    // GameTest world has blocks near y 3) (the probe's setBlock flags leave the
    // chunk heightmaps stale; *_WG ones use their own predicate on the
    // current blocks); SEA overrides the sea level. With a log stream every
    // heightmap and sea level read is printed.
    static Integer SEA;

    static final class Wrap implements InvocationHandler {
        final ServerLevel l;
        final PrintStream log;
        final int top;
        Wrap(ServerLevel l, PrintStream log, int top) { this.l = l; this.log = log; this.top = top; }

        @Override
        public Object invoke(Object p, Method m, Object[] a) throws Throwable {
            String n = m.getName();
            if (n.equals("getSeaLevel") && (SEA != null || System.getenv("SEA") != null)) {
                int v = SEA != null ? SEA : Integer.parseInt(System.getenv("SEA"));
                if (log != null) log.println("M getSeaLevel " + v);
                return v;
            }
            if ((n.equals("getHeight") && a != null && a.length == 3) || n.equals("getHeightmapPos")) {
                Heightmap.Types t = (Heightmap.Types) a[0];
                int x, z;
                if (a.length == 3) { x = (Integer) a[1]; z = (Integer) a[2]; } else { x = ((BlockPos) a[1]).getX(); z = ((BlockPos) a[1]).getZ(); }
                int y = top;
                while (y >= l.getMinY() && !t.isOpaque().test(l.getBlockState(new BlockPos(x, y, z)))) y--;
                y++;
                Object v = a.length == 3 ? (Object) y : new BlockPos(x, y, z);
                if (log != null) log.println("M " + n + " " + t + " " + x + " " + z + " -> " + y);
                return v;
            }
            if (log != null && (n.equals("getSeaLevel") || n.equals("getBiome") || n.equals("getMinY") || n.equals("getMaxY"))) log.println("M " + n);
            try {
                return m.invoke(l, a);
            } catch (InvocationTargetException e) {
                throw e.getCause();
            }
        }
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out; LogRandom.OUT = out;
        boolean log = System.getenv("FEAT_LOG") != null;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        init();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featdiskworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                var ops = RegistryOps.create(JsonOps.INSTANCE, level.registryAccess());
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ", 6);
                    String mode = a[0];
                    SEA = null;
                    if (mode.contains(":")) {
                        SEA = Integer.parseInt(mode.substring(mode.indexOf(':') + 1));
                        mode = mode.substring(0, mode.indexOf(':'));
                    }
                    int y0 = Integer.parseInt(a[1]);
                    long seed = Long.parseLong(a[2]);
                    int x = Integer.parseInt(a[3]), z = Integer.parseInt(a[4]);
                    for (int cx = (x - R - 8) >> 4; cx <= (x + R + 8) >> 4; cx++)
                        for (int cz = (z - R - 8) >> 4; cz <= (z + R + 8) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -R; dx <= R; dx++)
                        for (int dz = -R; dz <= R; dz++)
                            for (int dy = -R; dy <= R; dy++)
                                level.setBlock(new BlockPos(x + dx, y0 + dy, z + dz), scene(mode, seed, dx, dy, dz), 2 | 16);
                    var js = JsonParser.parseString(a[5]);
                    Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    r.on = log;
                    if (log) out.println("C " + line);
                    WorldGenLevel wl = (WorldGenLevel) Proxy.newProxyInstance(Place.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, new Wrap(level, log ? out : null, y0 + R));
                    boolean ok = cf.place(wl, level.getChunkSource().getGenerator(), r, new BlockPos(x, y0, z));
                    r.on = false;
                    StringBuilder sb = new StringBuilder(line).append("\t").append(ok);
                    for (int dy = -R; dy <= R; dy++)
                        for (int dx = -R; dx <= R; dx++)
                            for (int dz = -R; dz <= R; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(x + dx, y0 + dy, z + dz));
                                if (s != scene(mode, seed, dx, dy, dz)) sb.append(' ').append(dx).append(',').append(dy).append(',').append(dz).append('=').append(state(s));
                            }
                    out.println(sb.append(" | ").append(r.nextLong()));
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try {
            f.get();
        } finally {
            out.flush();
            Runtime.getRuntime().halt(0);
        }
    }
}
