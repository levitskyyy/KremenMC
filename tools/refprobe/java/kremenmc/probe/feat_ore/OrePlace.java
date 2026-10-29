package kremenmc.probe.feat_ore;

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
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.storage.LevelStorageSource;

// OrePlace places a configured feature (registry key string or inline JSON)
// at (x,y,z) inside a deterministic underground scene (see setup) on the
// flat test world, then prints every block of the box that differs from
// the scene, plus the random's next long. Input lines:
//   [log] <seed> <x> <y> <z> <json>
// With "log" every random call is printed first ("R ..." lines).
public final class OrePlace {
    static final int R = 16, Y0 = -20, Y1 = 30;

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

    // setup is the scene relative to the feature column (dx, dz) at world y.
    static BlockState setup(int dx, int y, int dz) {
        if (dx >= 2 && dx <= 5 && dz >= -3 && dz <= 1 && y >= 5 && y <= 8) return Blocks.AIR.defaultBlockState();
        if (dx >= -6 && dx <= -4 && dz >= 2 && dz <= 4 && y >= -2 && y <= -1) return Blocks.WATER.defaultBlockState();
        if (y == 2 || y == 3 || y == -6) return Blocks.TUFF.defaultBlockState();
        if (y == 10) return Blocks.GRANITE.defaultBlockState();
        if (y == -12) return Blocks.ANDESITE.defaultBlockState();
        if (y == 14) return Blocks.DIRT.defaultBlockState();
        if (y == 18) return Blocks.NETHERRACK.defaultBlockState();
        return y < 0 ? Blocks.DEEPSLATE.defaultBlockState() : Blocks.STONE.defaultBlockState();
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        OreLogRandom.OUT = out;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featoreworld-");
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
                    boolean log = line.startsWith("log ");
                    String[] a = (log ? line.substring(4) : line).split(" ", 5);
                    long seed = Long.parseLong(a[0]);
                    int x = Integer.parseInt(a[1]), y = Integer.parseInt(a[2]), z = Integer.parseInt(a[3]);
                    for (int cx = (x - R - 16) >> 4; cx <= (x + R + 16) >> 4; cx++)
                        for (int cz = (z - R - 16) >> 4; cz <= (z + R + 16) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -R; dx <= R; dx++)
                        for (int dz = -R; dz <= R; dz++)
                            for (int yy = Y0; yy <= Y1; yy++)
                                level.setBlock(new BlockPos(x + dx, yy, z + dz), setup(dx, yy, dz), 2 | 16);
                    var js = JsonParser.parseString(a[4]);
                    Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                    RandomSource base = new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed));
                    OreLogRandom lr = new OreLogRandom(base);
                    lr.on = log;
                    boolean ok = cf.place(level, level.getChunkSource().getGenerator(), lr, new BlockPos(x, y, z));
                    lr.on = false;
                    StringBuilder sb = new StringBuilder(log ? line.substring(4) : line).append("\t").append(ok);
                    for (int yy = Y0; yy <= Y1; yy++)
                        for (int dx = -R; dx <= R; dx++)
                            for (int dz = -R; dz <= R; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(x + dx, yy, z + dz));
                                if (s != setup(dx, yy, dz)) sb.append(' ').append(dx).append(',').append(yy).append(',').append(dz).append('=').append(state(s));
                            }
                    out.println(sb.append(" | ").append(lr.nextLong()));
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
