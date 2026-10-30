package kremenmc.probe.feat_ow;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
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
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.storage.LevelStorageSource;

// Place places a feature (registry key or inline JSON) at (x, y0, z) in a
// hashed scene and prints every block of a box of radius R (env R, default 24)
// that differs from the scene, then the random's next long. FEAT_LOG=1 logs
// every draw and level call. Input lines:
//   <mode> <y0> <seed> <x> <z> <feature>
// Modes: stone, holes (as feat_geode.Geode), cave / cavestone (a hashed
// deepslate / stone room with bumps, pillars and puddles), rootsN (a stone
// cave ceiling at dy 0 under N blocks of stone, dirt and grass), cfg.
// Mirrored by feature/ow_place_test.go.
public final class Place {
    static int R = 24;

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
        if (mode.equals("snow")) {
            if (dy < -1) return Blocks.STONE.defaultBlockState();
            if (dy == -1) return (hash(seed, dx, 3, dz) & 31) == 0 ? Blocks.STONE.defaultBlockState() : Blocks.SNOW_BLOCK.defaultBlockState();
            if (dy <= 1 && (hash(seed, dx, dy, dz) & 63) == 0) return Blocks.SNOW_BLOCK.defaultBlockState();
            return Blocks.AIR.defaultBlockState();
        }
        if (mode.equals("sand")) return dy < 0 ? (dy < -4 ? Blocks.SANDSTONE.defaultBlockState() : Blocks.SAND.defaultBlockState()) : Blocks.AIR.defaultBlockState();
        if (mode.equals("caveair")) return scene("cave", seed, dx, dy + 3, dz);
        if (mode.equals("cavezoo") || mode.equals("cavezoo2")) {
            // cave with one of 16 block kinds on a quarter of the floor cells
            BlockState b = scene("cave", seed, dx, dy, dz);
            if (!b.isAir()) return b;
            if (!scene("cave", seed, dx, dy - 1, dz).is(Blocks.DEEPSLATE)) return b;
            int k = (hash(seed, dx, 104, dz) >>> 4) & 63;
            if (k >= 16) return b;
            if (mode.equals("cavezoo2")) {
                String[] z2 = {"soul_fire", "fire", "fern", "dead_bush", "seagrass", "vine[north=true]", "leaf_litter", "hanging_roots",
                    "snow[layers=8]", "light", "short_dry_grass", "bush", "resin_clump[down=true]", "cave_air", "tall_seagrass", "warped_roots"};
                try {
                    return net.minecraft.commands.arguments.blocks.BlockStateParser.parseForBlock(BuiltInRegistries.BLOCK, "minecraft:" + z2[k], false).blockState();
                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
            }
            String[] zoo = {"snow", "deepslate_tile_stairs", "deepslate_tile_slab[type=top]", "glass", "oak_leaves[persistent=true]", "oak_fence",
                "torch", "poppy", "white_carpet", "cobweb", "water[level=3]", "lava", "short_grass", "oak_trapdoor", "moss_carpet", "glow_lichen[down=true]"};
            try {
                return net.minecraft.commands.arguments.blocks.BlockStateParser.parseForBlock(BuiltInRegistries.BLOCK, "minecraft:" + zoo[k], false).blockState();
            } catch (Exception e) {
                throw new RuntimeException(e);
            }
        }
        if (mode.equals("cavemix")) {
            // cave with snow layers and waterlogged stairs on the floor
            BlockState b = scene("cave", seed, dx, dy, dz);
            if (!b.isAir()) return b;
            if (!scene("cave", seed, dx, dy - 1, dz).is(Blocks.DEEPSLATE)) return b;
            return switch ((hash(seed, dx, 103, dz) >>> 4) & 7) {
                case 0 -> Blocks.SNOW.defaultBlockState();
                case 1 -> Blocks.SNOW.defaultBlockState().setValue(net.minecraft.world.level.block.SnowLayerBlock.LAYERS, 3);
                case 2 -> Blocks.DEEPSLATE_TILE_STAIRS.defaultBlockState().setValue(net.minecraft.world.level.block.StairBlock.WATERLOGGED, true);
                case 3 -> Blocks.DEEPSLATE_TILE_SLAB.defaultBlockState();
                default -> b;
            };
        }
        if (mode.startsWith("ocean")) {
            // ocean: water from the floor (dy -D, D after "ocean", default 20) up to dy 0, air above; stone below.
            int depth = mode.length() > 5 ? Integer.parseInt(mode.substring(5)) : 20;
            if (dy > 0) return Blocks.AIR.defaultBlockState();
            if (dy >= -depth) return Blocks.WATER.defaultBlockState();
            return dy == -depth - 1 && (hash(seed, dx, 7, dz) & 3) == 0 ? Blocks.GRAVEL.defaultBlockState() : Blocks.STONE.defaultBlockState();
        }
        if (mode.startsWith("dripcave")) {
            // large cave for large_dripstone (dripcave2: lower)
            int fh = hash(seed, dx, 100, dz) >>> 8;
            boolean low = mode.equals("dripcave2");
            int floor = (low ? -6 : -12) + (fh & 3);
            int ceil = (low ? 4 : 8) + ((fh >>> 2) & 7);
            if (Math.abs(dx) > 28 || Math.abs(dz) > 28 || dy < floor || dy >= ceil) {
                return switch ((hash(seed, dx, dy, dz) >>> 4) & 63) {
                    case 0 -> Blocks.DIRT.defaultBlockState();
                    case 1 -> Blocks.GRAVEL.defaultBlockState();
                    case 2 -> Blocks.GRANITE.defaultBlockState();
                    case 3 -> Blocks.LAVA.defaultBlockState();
                    case 4 -> Blocks.WATER.defaultBlockState();
                    case 5, 6 -> Blocks.DEEPSLATE.defaultBlockState();
                    case 7 -> Blocks.DRIPSTONE_BLOCK.defaultBlockState();
                    case 8 -> Blocks.AIR.defaultBlockState();
                    default -> Blocks.STONE.defaultBlockState();
                };
            }
            if ((hash(seed, dx, 101, dz) & 127) == 0) return Blocks.STONE.defaultBlockState();
            if (dy == floor) {
                int k = hash(seed, dx, 102, dz) & 15;
                if (k == 0) return Blocks.WATER.defaultBlockState();
                if (k == 1) return Blocks.LAVA.defaultBlockState();
            }
            return Blocks.AIR.defaultBlockState();
        }
        if (mode.startsWith("dripzoo")) {
            // mixed-rock cave for speleothem_cluster: varied floors and ceilings
            // (dripzoo2: some deep pits and tall ceilings), lava and water.
            int fh = hash(seed, dx, 100, dz) >>> 8;
            boolean tall = mode.equals("dripzoo2");
            int floor = tall && (fh & 7) == 0 ? -15 : (fh & 3) - 1;
            int ceil = tall ? 4 + ((fh >>> 3) & 15) : 4 + ((fh >>> 3) & 7);
            if (Math.abs(dx) > 12 || Math.abs(dz) > 12 || dy < floor || dy >= ceil) {
                return switch ((hash(seed, dx, dy, dz) >>> 4) & 31) {
                    case 0 -> Blocks.DIRT.defaultBlockState();
                    case 1 -> Blocks.GRAVEL.defaultBlockState();
                    case 2 -> Blocks.GRANITE.defaultBlockState();
                    case 3 -> Blocks.LAVA.defaultBlockState();
                    case 4 -> Blocks.WATER.defaultBlockState();
                    case 5 -> Blocks.TUFF.defaultBlockState();
                    case 6 -> Blocks.DEEPSLATE.defaultBlockState();
                    case 7 -> Blocks.COPPER_ORE.defaultBlockState();
                    case 8 -> Blocks.DRIPSTONE_BLOCK.defaultBlockState();
                    case 9 -> Blocks.CALCITE.defaultBlockState();
                    case 10 -> Blocks.GLASS.defaultBlockState();
                    default -> Blocks.STONE.defaultBlockState();
                };
            }
            if ((hash(seed, dx, 101, dz) & 63) == 0) return Blocks.STONE.defaultBlockState();
            if (dy == floor) {
                int k = hash(seed, dx, 102, dz) & 15;
                if (k == 0) return Blocks.WATER.defaultBlockState();
                if (k == 1) return Blocks.LAVA.defaultBlockState();
                if (k == 2) return Blocks.MOSS_CARPET.defaultBlockState();
            }
            if (dy == ceil - 1 && (hash(seed, dx, 105, dz) & 15) == 0) return Blocks.LAVA.defaultBlockState();
            return Blocks.AIR.defaultBlockState();
        }
        if (mode.equals("caveceil")) return scene("cavestone", seed, dx, dy + 6, dz);
        if (mode.equals("cavewetstone")) return scene("cavewet", seed, dx, dy, dz).is(Blocks.DEEPSLATE) ? Blocks.STONE.defaultBlockState() : scene("cavewet", seed, dx, dy, dz);
        if (mode.startsWith("cave")) {
            BlockState rock = mode.equals("cavestone") ? Blocks.STONE.defaultBlockState() : Blocks.DEEPSLATE.defaultBlockState();
            int fh = hash(seed, dx, 100, dz) >>> 8;
            int floor = (fh & 3) == 0 ? 1 : 0;
            int ceil = 7 + ((fh >>> 2) & 1);
            int half = mode.equals("cavebig") ? 20 : 10;
            if (Math.abs(dx) > half || Math.abs(dz) > half || dy < floor || dy >= ceil) {
                if (dy < -4 && (hash(seed, dx, dy, dz) & 15) == 0) return Blocks.TUFF.defaultBlockState();
                return rock;
            }
            if ((hash(seed, dx, 101, dz) & 63) == 0) return rock;
            if (dy == floor && (hash(seed, dx, 102, dz) & 31) == 0) return Blocks.WATER.defaultBlockState();
            if (mode.equals("cavewet") && dy <= 1) return Blocks.WATER.defaultBlockState();
            return Blocks.AIR.defaultBlockState();
        }
        if (mode.startsWith("roots")) {
            int n = Integer.parseInt(mode.substring(5));
            if (dy <= -6) return Blocks.STONE.defaultBlockState();
            if (dy < 0) return Blocks.AIR.defaultBlockState();
            if (dy >= n) return Blocks.AIR.defaultBlockState();
            if (dy == n - 1) return Blocks.GRASS_BLOCK.defaultBlockState();
            if (dy >= n - 3) return Blocks.DIRT.defaultBlockState();
            return Blocks.STONE.defaultBlockState();
        }
        if (mode.equals("holes")) {
            int h = (hash(seed, dx, dy, dz) >>> 8) & 63;
            if (h < 4) return Blocks.AIR.defaultBlockState();
            if (h < 8) return Blocks.WATER.defaultBlockState();
            if (h == 8) return Blocks.LAVA.defaultBlockState();
            if (h == 9) return Blocks.BEDROCK.defaultBlockState();
            if (h == 10) return Blocks.ICE.defaultBlockState();
            if (h == 11) return Blocks.DEEPSLATE.defaultBlockState();
            return Blocks.STONE.defaultBlockState();
        }
        if (mode.equals("snow")) {
            if (dy < -1) return Blocks.STONE.defaultBlockState();
            if (dy == -1) return (hash(seed, dx, 3, dz) & 31) == 0 ? Blocks.STONE.defaultBlockState() : Blocks.SNOW_BLOCK.defaultBlockState();
            if (dy <= 1 && (hash(seed, dx, dy, dz) & 63) == 0) return Blocks.SNOW_BLOCK.defaultBlockState();
            return Blocks.AIR.defaultBlockState();
        }
        if (mode.equals("carpet")) {
            if (dy < -1) return Blocks.STONE.defaultBlockState();
            if (dy == -1) return (hash(seed, dx, 7, dz) & 15) == 0 ? Blocks.AIR.defaultBlockState() : Blocks.PALE_MOSS_BLOCK.defaultBlockState();
            if (dx == 0 && dz == 0) {
                if (dy == 1) {
                    int u = hash(seed, 0, 9, 0) & 7;
                    if (u == 0) return Blocks.STONE.defaultBlockState();
                    if (u == 1) return Blocks.SHORT_GRASS.defaultBlockState();
                    if (u == 2) return Blocks.PALE_OAK_LEAVES.defaultBlockState();
                }
                return Blocks.AIR.defaultBlockState();
            }
            int h = (hash(seed, dx, 0, dz) >>> 4) & 15;
            int top = (hash(seed, dx, 1, dz) >>> 4) & 3;
            if (dy > top) return Blocks.AIR.defaultBlockState();
            return switch (h) {
                case 0, 1 -> Blocks.STONE.defaultBlockState();
                case 2 -> Blocks.PALE_OAK_LOG.defaultBlockState();
                case 3 -> Blocks.PALE_OAK_LEAVES.defaultBlockState();
                case 4 -> Blocks.GLASS.defaultBlockState();
                case 5 -> Blocks.OAK_FENCE.defaultBlockState();
                case 6 -> dy == 0 ? Blocks.PALE_MOSS_CARPET.defaultBlockState() : Blocks.AIR.defaultBlockState();
                case 7 -> dy == 0 ? Blocks.SHORT_GRASS.defaultBlockState() : Blocks.AIR.defaultBlockState();
                default -> Blocks.AIR.defaultBlockState();
            };
        }
        return Blocks.STONE.defaultBlockState();
    }

    static final class Wrap implements InvocationHandler {
        final ServerLevel l;
        final PrintStream log;
        BlockPos o = BlockPos.ZERO;
        boolean reads;
        Wrap(ServerLevel l, PrintStream log) { this.l = l; this.log = log; reads = log != null && "2".equals(System.getenv("FEAT_LOG")); }
        String rel(BlockPos b) { return (b.getX() - o.getX()) + "," + (b.getY() - o.getY()) + "," + (b.getZ() - o.getZ()); }

        @Override
        public Object invoke(Object p, Method m, Object[] a) throws Throwable {
            String n = m.getName();
            if (n.equals("setBlockAndUpdate")) {
                n = "setBlock";
                a = new Object[] {a[0], a[1], 3};
                m = net.minecraft.world.level.LevelWriter.class.getMethod("setBlock", BlockPos.class, BlockState.class, int.class);
                if (log != null) log.println("M setBlockAndUpdate");
            }
            if (reads && n.equals("isStateAtPosition") && a != null && a[0] instanceof BlockPos bp) {
                Object res;
                try { res = m.invoke(l, a); } catch (InvocationTargetException e) { throw e.getCause(); }
                log.println("S " + rel(bp) + " " + res + " @" + LogRandom.path());
                return res;
            }
            if (n.equals("getSeaLevel") && System.getenv("SEA") != null) {
                if (log != null) log.println("M getSeaLevel");
                return o.getY() + Integer.parseInt(System.getenv("SEA"));
            }
            if (log != null && n.equals("getHeight") && a != null && a.length == 3) {
                Object res;
                try { res = m.invoke(l, a); } catch (InvocationTargetException e) { throw e.getCause(); }
                log.println("M getHeight " + a[0] + " " + ((int) a[1] - o.getX()) + " " + ((int) a[2] - o.getZ()) + " = " + ((int) res - o.getY()));
                return res;
            }
            if (log != null && !n.equals("getBlockState") && !n.equals("setBlock") && !n.equals("getFluidState") && !n.equals("getChunk")) log.println("M " + n + (n.equals("levelEvent") ? " " + java.util.Arrays.toString(a).replaceAll("BlockPos\\{x=(-?\\d+), y=(-?\\d+), z=(-?\\d+)\\}", "$1 $2 $3") : ""));
            if (n.equals("setBlock") && a != null && a.length >= 3 && a[2] instanceof Integer fl) {
                if (log != null) log.println("W " + rel((BlockPos) a[0]) + " " + state((BlockState) a[1]) + " " + fl + " @" + LogRandom.path());
                a[2] = (fl & ~1) | 16;
            }
            if (reads && (n.equals("getBlockState") || n.equals("getFluidState")) && a != null && a.length == 1 && a[0] instanceof BlockPos bp)
                log.println((n.equals("getBlockState") ? "G " : "F ") + rel(bp) + " @" + LogRandom.path());
            if (n.equals("getRandom") && log != null) {
                LogRandom lr = new LogRandom(l.getRandom());
                lr.on = true;
                return lr;
            }
            try {
                return m.invoke(l, a);
            } catch (InvocationTargetException e) {
                throw e.getCause();
            }
        }
    }

    static void dump(PrintStream out, String pre, Object o, int depth) throws Exception {
        if (o == null || depth > 6) { out.println(pre + "=" + o); return; }
        Class<?> c = o.getClass();
        if (c.isPrimitive() || o instanceof Number || o instanceof Boolean || o instanceof String || c.isEnum()) { out.println(pre + "=" + o); return; }
        if (c.getName().startsWith("java.") || c.getName().startsWith("com.") || o instanceof BlockState) { out.println(pre + "=" + o); return; }
        boolean any = false;
        for (Class<?> k = c; k != null && k != Object.class; k = k.getSuperclass())
            for (Field f : k.getDeclaredFields()) {
                if (Modifier.isStatic(f.getModifiers())) continue;
                f.setAccessible(true);
                any = true;
                dump(out, pre + "." + f.getName(), f.get(o), depth + 1);
            }
        if (!any) out.println(pre + "=" + o);
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out; LogRandom.OUT = out;
        boolean log = System.getenv("FEAT_LOG") != null;
        if (System.getenv("R") != null) R = Integer.parseInt(System.getenv("R"));
        System.setOut(System.err);
        var lines = Files.readAllLines(Path.of(args[0]));
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featowworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                out.println("# level seed " + level.getSeed());
                var ops = RegistryOps.create(JsonOps.INSTANCE, level.registryAccess());
                for (String line : lines) {
                    if (line.isBlank() || line.startsWith("#")) continue;
                    String[] a = line.split(" ", 6);
                    String mode = a[0];
                    int y0 = Integer.parseInt(a[1]);
                    long seed = Long.parseLong(a[2]);
                    int x = Integer.parseInt(a[3]), z = Integer.parseInt(a[4]);
                    var js = JsonParser.parseString(a[5]);
                    Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                    if (mode.equals("cfg")) {
                        dump(out, "cfg", cf, 0);
                        continue;
                    }
                    for (int cx = (x - R - 8) >> 4; cx <= (x + R + 8) >> 4; cx++)
                        for (int cz = (z - R - 8) >> 4; cz <= (z + R + 8) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -R; dx <= R; dx++)
                        for (int dz = -R; dz <= R; dz++)
                            for (int dy = -R; dy <= R; dy++)
                                level.setBlock(new BlockPos(x + dx, y0 + dy, z + dz), scene(mode, seed, dx, dy, dz), 2 | 16);
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    r.on = log;
                    if (log) out.println("C " + line);
                    Wrap wrap = new Wrap(level, log ? out : null);
                    wrap.o = new BlockPos(x, y0, z);
                    WorldGenLevel wl = (WorldGenLevel) Proxy.newProxyInstance(Place.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, wrap);
                    boolean ok;
                    if (System.getenv("SPREAD") != null && cf instanceof net.minecraft.world.level.levelgen.feature.SculkPatchFeature spf) {
                        var sp = net.minecraft.world.level.block.SculkSpreader.createWorldGenSpreader();
                        BlockPos o = new BlockPos(x, y0, z);
                        out.println("P " + sp.replaceableBlocks().location() + " cost=" + sp.growthSpawnCost() + " nogrowth=" + sp.noGrowthRadius() + " decay=" + sp.chargeDecayRate() + " add=" + sp.additionalDecayRate());
                        boolean conv = !"0".equals(System.getenv("SPREAD"));
                        for (int i = 0; i < spf.chargeCount(); i++) sp.addCursors(o, spf.amountPerCharge());
                        for (int j = 0; j < spf.spreadAttempts(); j++) {
                            sp.updateCursors(wl, o, r, conv);
                            StringBuilder kb = new StringBuilder("K " + j + ":");
                            for (var cc : sp.getCursors()) {
                                int m = 0;
                                for (var d : cc.getFacingData()) m |= 1 << d.ordinal();
                                kb.append(' ').append(wrap.rel(cc.getPos())).append('/').append(cc.getCharge()).append('/').append(cc.getDecayDelay()).append('/').append(m);
                            }
                            out.println(kb);
                        }
                        ok = true;
                    } else {
                        var gen = level.getChunkSource().getGenerator();
                        if ("overworld".equals(System.getenv("GEN"))) {
                            // a noise generator with the overworld settings (sea level 63) instead of the flat one
                            var settings = level.registryAccess().lookupOrThrow(net.minecraft.core.registries.Registries.NOISE_SETTINGS)
                                .getOrThrow(net.minecraft.world.level.levelgen.NoiseGeneratorSettings.OVERWORLD);
                            gen = new net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator(gen.getBiomeSource(), settings);
                        }
                        ok = cf.place(wl, gen, r, new BlockPos(x, y0, z));
                    }
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
