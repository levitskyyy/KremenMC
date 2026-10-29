package kremenmc.probe.feat_ocean;

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
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.storage.LevelStorageSource;

// Place (ocean copy of feat_misc.Place; FEAT_LOG=1 logs every draw) places an inline feature (data pack JSON, Feature.DIRECT_CODEC) on
// the flat test world with a seeded xoroshiro WorldgenRandom at (x,-60,z) and
// prints every block of the box that differs from the setup, plus the
// random's next long. Input lines:
//   <setup> <seed> <x> <z> <json>
// setup: water (sand floor at -61, water -60..waterTop, air above) with
// waterTop given as water<N> (e.g. water-40), or air.
public final class Place {
    static boolean LOG;
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

    static BlockState setup(String s, int y) {
        if (y == -62) return Blocks.STONE.defaultBlockState();
        if (s.equals("snow")) return y == -61 ? Blocks.SNOW_BLOCK.defaultBlockState() : Blocks.AIR.defaultBlockState();
        if (s.startsWith("packed")) {
            int top = Integer.parseInt(s.substring(6));
            if (y <= top) return Blocks.PACKED_ICE.defaultBlockState();
            return y <= -40 ? Blocks.WATER.defaultBlockState() : Blocks.AIR.defaultBlockState();
        }
        if (y == -61) return Blocks.SAND.defaultBlockState();
        if (s.startsWith("water")) {
            int top = Integer.parseInt(s.substring(5));
            if (y <= top) return Blocks.WATER.defaultBlockState();
        }
        return Blocks.AIR.defaultBlockState();
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out; LogRandom.OUT = out; LOG = System.getenv("FEAT_LOG") != null;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featoceanworld-");
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
                    String[] a = line.split(" ", 5);
                    long seed = Long.parseLong(a[1]);
                    int x = Integer.parseInt(a[2]), z = Integer.parseInt(a[3]);
                    for (int cx = (x - 12) >> 4; cx <= (x + 12) >> 4; cx++)
                        for (int cz = (z - 12) >> 4; cz <= (z + 12) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -12; dx <= 12; dx++)
                        for (int dz = -12; dz <= 12; dz++)
                            for (int y = -62; y <= -20; y++)
                                level.setBlock(new BlockPos(x + dx, y, z + dz), setup(a[0], y), 2 | 16);
                    var js = JsonParser.parseString(a[4]);
                    Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed))); r.on = LOG; if (LOG) out.println("C " + line);
                    boolean ok = cf.place(level, level.getChunkSource().getGenerator(), r, new BlockPos(x, -60, z));
                    StringBuilder sb = new StringBuilder(a[0]).append(' ').append(a[1]).append(' ').append(a[2]).append(' ').append(a[3]).append(" => ").append(ok);
                    for (int y = -62; y <= -20; y++)
                        for (int dx = -12; dx <= 12; dx++)
                            for (int dz = -12; dz <= 12; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(x + dx, y, z + dz));
                                if (s != setup(a[0], y)) sb.append(' ').append(dx).append(',').append(y).append(',').append(dz).append('=').append(state(s));
                            }
                    r.on = false; out.println(sb.append(" | ").append(r.nextLong()));
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
