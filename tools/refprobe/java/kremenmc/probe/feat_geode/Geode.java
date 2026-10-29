package kremenmc.probe.feat_geode;

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

// Geode places a feature (registry key or inline JSON) at (x, y0, z) in a
// hashed scene and prints every block of a box of radius R (env R, default 24)
// that differs from the scene, then the random's next long. FEAT_LOG=1 logs
// every draw and level call. Input lines:
//   <mode> <y0> <seed> <x> <z> <feature>
// Modes: stone (all stone), holes (stone with hashed air, water, lava,
// bedrock and ice cells: 1 in 16 each of air/water, 1 in 64 others),
// cfg (prints the parsed configuration by reflection, places nothing).
// Mirrored by feature/geode_test.go.
public final class Geode {
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
        return Blocks.STONE.defaultBlockState();
    }

    static final class Wrap implements InvocationHandler {
        final ServerLevel l;
        final PrintStream log;
        Wrap(ServerLevel l, PrintStream log) { this.l = l; this.log = log; }

        @Override
        public Object invoke(Object p, Method m, Object[] a) throws Throwable {
            String n = m.getName();
            if (log != null && !n.equals("getBlockState") && !n.equals("setBlock") && !n.equals("getFluidState")) log.println("M " + n);
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
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featgeodeworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
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
                    WorldGenLevel wl = (WorldGenLevel) Proxy.newProxyInstance(Geode.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, new Wrap(level, log ? out : null));
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
