package kremenmc.probe.feat_cave;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.storage.LevelStorageSource;

// Draws is Place with every random call logged (see LogRandom). Place places a registered feature (Feature.place) at (x, -30, z) inside a
// deterministic 33^3 scene (see scene) with a seeded xoroshiro
// WorldgenRandom, and prints every block that differs from the scene plus
// the random's next long. Input lines: <feature key> <seed> <x> <z>
public final class Draws {
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

    // scene is the deterministic test scene: air with scattered solids,
    // denser away from the centre; mirrored by the Go tests.
    static BlockState scene(long seed, int dx, int dy, int dz, int R) {
        if (Math.abs(dx) == R || Math.abs(dy) == R || Math.abs(dz) == R) return Blocks.STONE.defaultBlockState();
        int h = dx * 73856093 ^ dy * 19349663 ^ dz * 83492791 ^ (int) seed;
        h ^= h >>> 13; h *= 0x5bd1e995; h ^= h >>> 15;
        int k = (h >>> 4) & 7;
        if (k >= 2) return Blocks.AIR.defaultBlockState();
        int t = (h >>> 8) & 7;
        return switch (t) {
            case 0, 1, 2 -> Blocks.STONE.defaultBlockState();
            case 3 -> Blocks.DEEPSLATE.defaultBlockState();
            case 4 -> Blocks.DIRT.defaultBlockState();
            case 5 -> Blocks.OAK_LEAVES.defaultBlockState();
            case 6 -> Blocks.WATER.defaultBlockState();
            default -> Blocks.MOSS_BLOCK.defaultBlockState();
        };
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        LogRandom.OUT = out;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featureworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                var features = level.registryAccess().lookupOrThrow(Registries.FEATURE);
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ");
                    long seed = Long.parseLong(a[1]);
                    int x = Integer.parseInt(a[2]), z = Integer.parseInt(a[3]);
                    int R = 16, Y0 = -30;
                    for (int cx = (x - R) >> 4; cx <= (x + R) >> 4; cx++)
                        for (int cz = (z - R) >> 4; cz <= (z + R) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -R; dx <= R; dx++)
                        for (int dz = -R; dz <= R; dz++)
                            for (int dy = -R; dy <= R; dy++)
                                level.setBlock(new BlockPos(x + dx, Y0 + dy, z + dz), scene(seed, dx, dy, dz, R), 2 | 16);
                    var feat = features.getOrThrow(ResourceKey.create(Registries.FEATURE, Identifier.parse(a[0]))).value();
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    boolean ok = feat.place(level, level.getChunkSource().getGenerator(), r, new BlockPos(x, Y0, z));
                    StringBuilder sb = new StringBuilder(line).append(" => ").append(ok);
                    for (int dy = -R; dy <= R; dy++)
                        for (int dx = -R; dx <= R; dx++)
                            for (int dz = -R; dz <= R; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(x + dx, Y0 + dy, z + dz));
                                if (s != scene(seed, dx, dy, dz, R)) sb.append(' ').append(dx).append(',').append(dy).append(',').append(dz).append('=').append(state(s));
                            }
                    r.on = false;
                    out.println(sb.append(" | ").append(r.nextLong()));
                }
                f.complete(null);
            } catch (Throwable t) {
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
