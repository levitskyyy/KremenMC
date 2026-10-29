package kremenmc.probe.feat_env;

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
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.RegistryOps;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.storage.LevelStorageSource;

// Place places a feature (registry key or inline JSON) at (x, y0, z) in a
// deterministic scene with a seeded xoroshiro WorldgenRandom and prints every
// block of the 33^3 box that differs from the scene plus the random's next
// long. The scene's chunks get a uniform biome. Input lines:
//   <mode> <biome> <y0> <seed> <x> <z> <feature>
// Modes: cave (solids, 1/4 air), solid (rock, 1/16 air), solid0 (rock),
// tunnel (rock with a 2-high air tunnel along x), ocean (stone floor, water
// to y0+8), terrain/dry (stone columns topped by a hashed surface block, dry
// without water). Mirrored by feature/env_test.
public final class Place {
    static final int R = 16;

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
        if (mode.equals("cave")) {
            if (Math.abs(dx) == R || Math.abs(dy) == R || Math.abs(dz) == R) return Blocks.STONE.defaultBlockState();
            int h = hash(seed, dx, dy, dz);
            if (((h >>> 4) & 3) == 0) return Blocks.AIR.defaultBlockState();
            return switch ((h >>> 8) & 7) {
                case 0, 1, 2 -> Blocks.STONE.defaultBlockState();
                case 3 -> Blocks.DEEPSLATE.defaultBlockState();
                case 4 -> Blocks.DIRT.defaultBlockState();
                case 5 -> Blocks.GRANITE.defaultBlockState();
                case 6 -> Blocks.WATER.defaultBlockState();
                default -> Blocks.AIR.defaultBlockState();
            };
        }
        if (mode.startsWith("solid") || mode.equals("tunnel")) {
            int h = hash(seed, dx, dy, dz);
            if (mode.equals("solid") && ((h >>> 4) & 15) == 0) return Blocks.AIR.defaultBlockState();
            if (mode.equals("tunnel") && (dy == 0 || dy == 1) && dz == (int) Math.floorMod(seed, 9L) - 4) return Blocks.AIR.defaultBlockState();
            return switch ((h >>> 8) & 7) {
                case 0, 1, 2 -> Blocks.STONE.defaultBlockState();
                case 3 -> Blocks.DEEPSLATE.defaultBlockState();
                case 4 -> Blocks.DIRT.defaultBlockState();
                case 5 -> Blocks.GRANITE.defaultBlockState();
                case 6 -> Blocks.TUFF.defaultBlockState();
                default -> Blocks.ANDESITE.defaultBlockState();
            };
        }
        if (mode.equals("ocean")) {
            int floor = -1 - ((hash(seed, dx, 0, dz) >>> 4) & 7);
            if (dy <= floor) return Blocks.STONE.defaultBlockState();
            if (dy <= 8) return Blocks.WATER.defaultBlockState();
            return Blocks.AIR.defaultBlockState();
        }
        int h = hash(seed, dx, 0, dz);
        int top = (h >>> 4) & 7;
        if (dy > top) return Blocks.AIR.defaultBlockState();
        int kind = (h >>> 8) & 7;
        if (mode.equals("dry") && kind == 2) kind = 0;
        if (dy == top) {
            return switch (kind) {
                case 0, 1 -> Blocks.GRASS_BLOCK.defaultBlockState();
                case 2 -> Blocks.WATER.defaultBlockState();
                case 3 -> Blocks.SAND.defaultBlockState();
                case 4 -> Blocks.OAK_LEAVES.defaultBlockState();
                case 5 -> Blocks.ICE.defaultBlockState();
                case 6 -> Blocks.PODZOL.defaultBlockState();
                default -> Blocks.SHORT_GRASS.defaultBlockState();
            };
        }
        if (kind == 2 && dy == top - 1) return Blocks.WATER.defaultBlockState();
        if (kind == 7 && dy == top - 1) return Blocks.GRASS_BLOCK.defaultBlockState();
        return Blocks.STONE.defaultBlockState();
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featenvworld-");
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
                var biomes = level.registryAccess().lookupOrThrow(Registries.BIOME);
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ", 7);
                    String mode = a[0];
                    var biome = biomes.getOrThrow(ResourceKey.create(Registries.BIOME, Identifier.parse(a[1])));
                    int y0 = Integer.parseInt(a[2]);
                    long seed = Long.parseLong(a[3]);
                    int x = Integer.parseInt(a[4]), z = Integer.parseInt(a[5]);
                    for (int cx = (x - R - 8) >> 4; cx <= (x + R + 8) >> 4; cx++)
                        for (int cz = (z - R - 8) >> 4; cz <= (z + R + 8) >> 4; cz++)
                            level.getChunk(cx, cz).fillBiomesFromNoise((qx, qy, qz) -> biome);
                    for (int dx = -R; dx <= R; dx++)
                        for (int dz = -R; dz <= R; dz++)
                            for (int dy = -R; dy <= R; dy++)
                                level.setBlock(new BlockPos(x + dx, y0 + dy, z + dz), scene(mode, seed, dx, dy, dz), 2 | 16);
                    var js = JsonParser.parseString(a[6]);
                    Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                    var r = new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed));
                    boolean ok = cf.place(level, level.getChunkSource().getGenerator(), r, new BlockPos(x, y0, z));
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
