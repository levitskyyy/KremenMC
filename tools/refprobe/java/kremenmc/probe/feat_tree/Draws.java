package kremenmc.probe.feat_tree;

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

// Tree places registered features (Feature.place) on the flat test world
// with a seeded xoroshiro WorldgenRandom and prints every non-air block of
// the surrounding box afterwards plus the random's next long. Input lines:
//   <feature key> <seed> <x> <z> [dx,dy,dz=block ...]
// (optional obstacles, dy relative to the feature origin at y -60; the
// token prime also clears the box above it up to y 16, so the heightmaps
// sit on the probe's ground).
// The box (x-12..x+12, -62..-20, z-12..z+12) is reset to the flat layers
// (grass_block at -61, air above) before each case.
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
                    int oy = -60;
                    for (int i = 4; i < a.length; i++) if (a[i].startsWith("at=")) oy = Integer.parseInt(a[i].substring(3));
                    final int lo = oy - 2, hi = oy + 40;
                    for (int cx = (x - 12) >> 4; cx <= (x + 12) >> 4; cx++)
                        for (int cz = (z - 12) >> 4; cz <= (z + 12) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -12; dx <= 12; dx++)
                        for (int dz = -12; dz <= 12; dz++)
                            for (int y = lo; y <= hi; y++)
                                level.setBlock(new BlockPos(x + dx, y, z + dz), y == lo ? Blocks.DIRT.defaultBlockState() : y == lo + 1 ? Blocks.GRASS_BLOCK.defaultBlockState() : Blocks.AIR.defaultBlockState(), 2 | 16);
                    boolean prime = false;
                    for (int i = 4; i < a.length; i++) {
                        if (a[i].equals("prime")) { prime = true; continue; }
                        if (a[i].startsWith("at=")) continue;
                        String[] kv = a[i].split("=");
                        String[] c = kv[0].split(",");
                        var blk = BuiltInRegistries.BLOCK.getValue(Identifier.parse(kv[1]));
                        level.setBlock(new BlockPos(x + Integer.parseInt(c[0]), oy + Integer.parseInt(c[1]), z + Integer.parseInt(c[2])), blk.defaultBlockState(), 2 | 16);
                    }
                    if (prime) {
                        for (int cx = (x - 12) >> 4; cx <= (x + 12) >> 4; cx++)
                            for (int cz = (z - 12) >> 4; cz <= (z + 12) >> 4; cz++) {
                                var ch = level.getChunk(cx, cz);
                                if (cx == (x >> 4) && cz == (z >> 4)) {
                                    StringBuilder hb = new StringBuilder("heightmaps before:");
                                    for (var e : ch.getHeightmaps()) hb.append(' ').append(e.getKey()).append('=').append(ch.getHeight(e.getKey(), x & 15, z & 15));
                                    LogRandom.OUT.println("# " + hb);
                                }
                            }
                        for (int dx = -12; dx <= 12; dx++)
                            for (int dz = -12; dz <= 12; dz++)
                                for (int y = -19; y <= 16; y++)
                                    level.setBlock(new BlockPos(x + dx, y, z + dz), Blocks.AIR.defaultBlockState(), 2 | 16);
                        var ch = level.getChunk(x >> 4, z >> 4);
                        StringBuilder hb = new StringBuilder("heightmaps after:");
                        for (var e : ch.getHeightmaps()) hb.append(' ').append(e.getKey()).append('=').append(ch.getHeight(e.getKey(), x & 15, z & 15));
                        LogRandom.OUT.println("# " + hb);
                    }
                    var feat = features.getOrThrow(ResourceKey.create(Registries.FEATURE, Identifier.parse(a[0]))).value();
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    boolean ok = feat.place(level, level.getChunkSource().getGenerator(), r, new BlockPos(x, oy, z));
                    StringBuilder sb = new StringBuilder(line).append(" => ").append(ok);
                    for (int y = lo; y <= hi; y++)
                        for (int dx = -12; dx <= 12; dx++)
                            for (int dz = -12; dz <= 12; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(x + dx, y, z + dz));
                                boolean ground = (y == lo && s.is(Blocks.DIRT)) || (y == lo + 1 && s.is(Blocks.GRASS_BLOCK));
                                if (!s.isAir() && !ground) sb.append(' ').append(dx).append(',').append(y - oy - 60).append(',').append(dz).append('=').append(state(s));
                            }
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
