package kremenmc.probe.feat_nether;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
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
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.levelgen.placement.PlacedFeature;
import net.minecraft.world.level.storage.LevelStorageSource;

// Place places a feature or a placed feature (inline JSON with a "feature"
// key; drop "biome" modifiers, the inline feature is in no biome) at
// (x, y0, z) in a hashed nether or end scene of radius R with a seeded
// xoroshiro WorldgenRandom, and prints every block of the box that differs
// from the scene plus the random's next long. FEAT_LOG=1 logs every draw.
// Input lines: <mode>[:<R>] <y0> <seed> <x> <z> <json>
// Modes (scene relative to the origin, column hash h):
//   cave    netherrack with two air layers [-10+a, -3+b) and [1+c, 9+d),
//           hashed floor and ceiling blocks, lava in the lower layer of
//           some columns, sparse netherrack obstacles in the air;
//   open    a hashed floor block at -1 or -2 over netherrack, air above;
//   openc, openw  open with crimson / warped nylium at (0,-1,0);
//   openfc, openfw  the same with hashed fire, soul fire, roots, sprouts
//           or mushrooms on the floor;
//   bed     open with bedrock instead of netherrack as the floor block;
//   sea     lava up to -2 in most columns, basalt/netherrack/blackstone
//           islands rising to 0..2 elsewhere, air above;
//   ceil    netherrack or wart ceiling from 1 or 2 up, air below, a
//           netherrack floor at -R;
//   end     end_stone up to a hashed height -3..0, air above;
//   grass   grass block at -1 over dirt, air above;
//   grasso  grass with sparse stone obstacles from 2 up (none on the
//           origin column);
//   flat    a hashed solid floor block (netherrack, basalt, blackstone,
//           magma) at -1 over netherrack, air above;
//   pits    a hashed top at -2..1 (mostly -1) of netherrack, basalt,
//           blackstone, magma, soul sand or lava over netherrack, with
//           bedrock under some tops, air above;
//   air     air.
// Mirrored by internal/world/worldgen/feature/nether_place_test.go.
public final class Place {
    static int R = 16;
    static final boolean READS = System.getenv("FEAT_READS") != null;
    // TICKS collects scheduled ticks of the current case relative to its origin.
    static StringBuilder TICKS;
    static int OX, OY, OZ;
    static Block[] FLOOR, CEIL, ISLE, PLANT, FLAT, PITS;
    static final int[] PITS_T = {-2, -1, -1, -1, -1, 0, 0, 1};
    static final boolean BE = System.getenv("FEAT_BE") != null;

    static void init() {
        FLOOR = new Block[]{Blocks.NETHERRACK, Blocks.CRIMSON_NYLIUM, Blocks.WARPED_NYLIUM, Blocks.SOUL_SAND,
            Blocks.SOUL_SOIL, Blocks.BASALT, Blocks.BLACKSTONE, Blocks.CRIMSON_NYLIUM};
        CEIL = new Block[]{Blocks.NETHERRACK, Blocks.NETHER_WART_BLOCK, Blocks.WARPED_WART_BLOCK, Blocks.NETHERRACK};
        PLANT = new Block[]{Blocks.FIRE, Blocks.SOUL_FIRE, Blocks.CRIMSON_ROOTS, Blocks.NETHER_SPROUTS, Blocks.WARPED_ROOTS, Blocks.BROWN_MUSHROOM, Blocks.AIR, Blocks.AIR};
        ISLE = new Block[]{Blocks.BASALT, Blocks.NETHERRACK, Blocks.BLACKSTONE, Blocks.MAGMA_BLOCK};
        FLAT = new Block[]{Blocks.NETHERRACK, Blocks.BASALT, Blocks.BLACKSTONE, Blocks.MAGMA_BLOCK};
        PITS = new Block[]{Blocks.NETHERRACK, Blocks.BASALT, Blocks.BLACKSTONE, Blocks.MAGMA_BLOCK, Blocks.SOUL_SAND, Blocks.LAVA, Blocks.BASALT, Blocks.NETHERRACK};
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

    static BlockState s(Block b) { return b.defaultBlockState(); }

    static BlockState scene(String mode, long seed, int dx, int dy, int dz) {
        int h = hash(seed, dx, 0, dz);
        switch (mode) {
            case "air":
                return s(Blocks.AIR);
            case "openc", "openw", "openfc", "openfw":
                if (dx == 0 && dz == 0) {
                    if (dy == -1) return s(mode.endsWith("c") ? Blocks.CRIMSON_NYLIUM : Blocks.WARPED_NYLIUM);
                    if (dy >= 0) return s(Blocks.AIR);
                }
                // fall through
            case "open", "bed": {
                int f = -1 - ((h >>> 4) & 1);
                if (dy == f + 1 && mode.startsWith("openf")) return s(PLANT[(h >>> 20) & 7]);
                if (dy > f) return s(Blocks.AIR);
                if (dy == f && mode.equals("bed") && ((h >>> 13) & 7) == 0) return s(Blocks.BEDROCK);
                if (dy == f) return s(FLOOR[(h >>> 13) & 7]);
                return s(Blocks.NETHERRACK);
            }
            case "sea": {
                boolean isle = ((h >>> 4) & 3) == 0;
                if (isle) {
                    int top = (h >>> 6) & 3;
                    if (top == 3) top = 0;
                    return dy <= top ? s(ISLE[(h >>> 13) & 3]) : s(Blocks.AIR);
                }
                if (dy <= -2) return dy < -8 ? s(Blocks.NETHERRACK) : s(Blocks.LAVA);
                return s(Blocks.AIR);
            }
            case "ceil": {
                int c = 1 + ((h >>> 4) & 1);
                if (dy >= c) return dy == c ? s(CEIL[(h >>> 16) & 3]) : s(Blocks.NETHERRACK);
                if (dy == -R) return s(Blocks.NETHERRACK);
                return s(Blocks.AIR);
            }
            case "grasso":
                if (dy >= 2 && !(dx == 0 && dz == 0) && ((hash(seed, dx, dy, dz) >>> 4) & 15) == 0) return s(Blocks.STONE);
                // fall through
            case "grass":
                if (dy >= 0) return s(Blocks.AIR);
                return dy == -1 ? s(Blocks.GRASS_BLOCK) : s(Blocks.DIRT);
            case "flat":
                if (dy >= 0) return s(Blocks.AIR);
                if (dy == -1) return s(FLAT[(h >>> 13) & 3]);
                return s(Blocks.NETHERRACK);
            case "pits": {
                int t = PITS_T[(h >>> 4) & 7];
                if (dy > t) return s(Blocks.AIR);
                if (dy == t) return s(PITS[(h >>> 13) & 7]);
                if (dy == t - 1 && ((h >>> 20) & 15) == 0) return s(Blocks.BEDROCK);
                return s(Blocks.NETHERRACK);
            }
            case "end": {
                int top = -3 + ((h >>> 4) & 3);
                return dy <= top ? s(Blocks.END_STONE) : s(Blocks.AIR);
            }
        }
        int fA = -10 + ((h >>> 4) & 3), cA = -3 + ((h >>> 6) & 3);
        int fB = 1 + ((h >>> 8) & 3), cB = 9 + ((h >>> 10) & 7);
        boolean inA = dy >= fA && dy < cA, inB = dy >= fB && dy < cB;
        if (inA || inB) {
            if (inA && dy < fA + 2 && ((h >>> 18) & 3) == 0) return s(Blocks.LAVA);
            if (((hash(seed, dx, dy, dz) >>> 4) & 63) == 0) return s(Blocks.NETHERRACK);
            return s(Blocks.AIR);
        }
        if (dy == fA - 1 || dy == fB - 1) return s(FLOOR[(h >>> 13) & 7]);
        if (dy == cA || dy == cB) return s(CEIL[(h >>> 16) & 3]);
        return s(Blocks.NETHERRACK);
    }

    // Wrap hands the feature the ServerLevel as a WorldGenLevel; heightmaps
    // are recomputed by scanning down from the top of the scene box.
    static final class Wrap implements InvocationHandler {
        final ServerLevel l;
        final PrintStream log;
        final int top;
        Wrap(ServerLevel l, PrintStream log, int top) { this.l = l; this.log = log; this.top = top; }

        @Override
        public Object invoke(Object p, Method m, Object[] a) throws Throwable {
            String n = m.getName();
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
            if (log != null && System.getenv("FEAT_CALLS") != null) log.println("X " + n + " " + (a == null ? 0 : a.length));
            if (TICKS != null && n.equals("scheduleTick")) {
                BlockPos q = (BlockPos) a[0];
                Object k = a[1];
                String id = k instanceof net.minecraft.world.level.material.Fluid fl ? "fluid " + BuiltInRegistries.FLUID.getKey(fl) : "block " + BuiltInRegistries.BLOCK.getKey((Block) k);
                if (log != null) log.println("T " + q.getX() + " " + q.getY() + " " + q.getZ() + " " + a[2]);
                TICKS.append(' ').append(q.getX() - OX).append(',').append(q.getY() - OY).append(',').append(q.getZ() - OZ).append('=').append(id.replace(' ', ':')).append('/').append(a[2]);
                if (a.length > 3) TICKS.append('/').append(a[3]);
            }
            if (log != null && (n.equals("setBlock") || n.equals("setBlockAndUpdate"))) log.println("W " + ((BlockPos) a[0]).getX() + " " + ((BlockPos) a[0]).getY() + " " + ((BlockPos) a[0]).getZ() + " " + state((BlockState) a[1]));
            if (log != null && READS && (n.equals("getBlockState") || n.equals("getFluidState") || n.equals("isStateAtPosition") || n.equals("isEmptyBlock"))) log.println("B " + n + " " + ((BlockPos) a[0]).getX() + " " + ((BlockPos) a[0]).getY() + " " + ((BlockPos) a[0]).getZ() + (n.equals("getBlockState") ? " " + state(l.getBlockState((BlockPos) a[0])) : ""));
            if (log != null && (n.equals("getSeaLevel") || n.equals("getBiome") || n.equals("getMinY") || n.equals("getMaxY") || n.equals("getHeight"))) {
                Object v;
                try { v = m.invoke(l, a); } catch (InvocationTargetException e) { throw e.getCause(); }
                if (!n.equals("getMinY") || System.getenv("FEAT_MINY") != null) log.println("M " + n + " -> " + v);
                return v;
            }
            try {
                return m.invoke(l, a);
            } catch (InvocationTargetException e) {
                throw e.getCause();
            }
        }
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        LogRandom.OUT = out;
        boolean log = System.getenv("FEAT_LOG") != null;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        init();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featnetherworld-");
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
                    if (line.isBlank() || line.startsWith("#")) continue;
                    String[] a = line.split(" ", 6);
                    String mode = a[0];
                    R = 16;
                    if (mode.contains(":")) {
                        R = Integer.parseInt(mode.substring(mode.indexOf(':') + 1));
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
                    // The world may reject or rewrite scene blocks (survival,
                    // fire variants): diff against what it holds and report
                    // those cells as overrides after a second tab.
                    java.util.Map<BlockPos, BlockState> base = new java.util.HashMap<>();
                    StringBuilder ov = new StringBuilder();
                    for (int dy = -R; dy <= R; dy++)
                        for (int dx = -R; dx <= R; dx++)
                            for (int dz = -R; dz <= R; dz++) {
                                BlockPos q = new BlockPos(x + dx, y0 + dy, z + dz);
                                BlockState st = level.getBlockState(q);
                                base.put(q, st);
                                if (st != scene(mode, seed, dx, dy, dz)) ov.append(' ').append(dx).append(',').append(dy).append(',').append(dz).append('=').append(state(st));
                            }
                    if (System.getenv("FEAT_COLUMN") != null) {
                        StringBuilder col = new StringBuilder("COL");
                        for (int yy = level.getMinY(); yy < y0 - R; yy++) {
                            BlockState st = level.getBlockState(new BlockPos(x, yy, z));
                            if (!st.isAir()) col.append(' ').append(yy).append('=').append(state(st));
                        }
                        out.println(col);
                    }
                    var js = JsonParser.parseString(a[5]);
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    r.on = log;
                    if (log) out.println("C " + line);
                    TICKS = new StringBuilder();
                    OX = x; OY = y0; OZ = z;
                    WorldGenLevel wl = (WorldGenLevel) Proxy.newProxyInstance(Place.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, new Wrap(level, log ? out : null, y0 + R));
                    BlockPos origin = new BlockPos(x, y0, z);
                    boolean ok;
                    if (js.isJsonObject() && js.getAsJsonObject().has("feature")) {
                        PlacedFeature pf = PlacedFeature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                        ok = pf.place(wl, level.getChunkSource().getGenerator(), r, origin);
                    } else {
                        Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                        ok = cf.place(wl, level.getChunkSource().getGenerator(), r, origin);
                    }
                    r.on = false;
                    StringBuilder sb = new StringBuilder(line).append("\t").append(ok);
                    for (int dy = -R; dy <= R; dy++)
                        for (int dx = -R; dx <= R; dx++)
                            for (int dz = -R; dz <= R; dz++) {
                                BlockPos q = new BlockPos(x + dx, y0 + dy, z + dz);
                                BlockState st = level.getBlockState(q);
                                if (st != base.get(q)) sb.append(' ').append(dx).append(',').append(dy).append(',').append(dz).append('=').append(state(st));
                            }
                    sb.append(" | ").append(r.nextLong());
                    if (BE)
                        for (int dy = -R; dy <= R; dy++)
                            for (int dx = -R; dx <= R; dx++)
                                for (int dz = -R; dz <= R; dz++) {
                                    BlockPos q = new BlockPos(x + dx, y0 + dy, z + dz);
                                    var be = level.getBlockEntity(q);
                                    if (be != null && level.getBlockState(q) != base.get(q))
                                        sb.append(" be").append(dx).append(',').append(dy).append(',').append(dz).append('=').append(be.saveWithoutMetadata(level.registryAccess()));
                                }
                    if (TICKS.length() > 0) sb.append(" ticks").append(TICKS);
                    if (ov.length() > 0) sb.append('\t').append(ov.substring(1));
                    out.println(sb);
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
