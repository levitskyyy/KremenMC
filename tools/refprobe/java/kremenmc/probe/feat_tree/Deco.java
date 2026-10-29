package kremenmc.probe.feat_tree;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashSet;
import java.util.Optional;
import java.util.Set;
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
import net.minecraft.world.level.levelgen.feature.TreeFeature;
import net.minecraft.world.level.levelgen.feature.treedecorators.TreeDecorator;
import net.minecraft.world.level.storage.LevelStorageSource;

// Deco runs one decorator of a registered tree feature on a hand-made
// TreeDecorator.Context: the box (x±12, z±12, y -62..-20) is reset to the
// flat layers (grass at -61), the listed blocks are set, and the decorator
// gets the listed logs/leaves/roots. Every setter call and random draw is
// printed. Input lines:
//   <tree feature key> <decorator index> <seed> <x> <z> logs=dx,dy,dz/... [leaves=...] [roots=...] [set=dx,dy,dz=block/...]
// (dy relative to y -60; logs/leaves are also placed as blocks).
public final class Deco {
    static Set<BlockPos> list(String s, int x, int z) {
        Set<BlockPos> out = new java.util.LinkedHashSet<>();
        if (s == null || s.isEmpty()) return out;
        for (String p : s.split("/")) {
            String[] c = p.split(",");
            out.add(new BlockPos(x + Integer.parseInt(c[0]), -60 + Integer.parseInt(c[1]), z + Integer.parseInt(c[2])));
        }
        return out;
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
                    int idx = Integer.parseInt(a[1]);
                    long seed = Long.parseLong(a[2]);
                    int x = Integer.parseInt(a[3]), z = Integer.parseInt(a[4]);
                    String logs = null, leaves = null, roots = null, set = null;
                    for (int i = 5; i < a.length; i++) {
                        String[] kv = a[i].split("=", 2);
                        switch (kv[0]) {
                            case "logs" -> logs = kv[1];
                            case "leaves" -> leaves = kv[1];
                            case "roots" -> roots = kv[1];
                            case "set" -> set = kv[1];
                            default -> {}
                        }
                    }
                    for (int cx = (x - 12) >> 4; cx <= (x + 12) >> 4; cx++)
                        for (int cz = (z - 12) >> 4; cz <= (z + 12) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -12; dx <= 12; dx++)
                        for (int dz = -12; dz <= 12; dz++)
                            for (int y = -62; y <= -20; y++)
                                level.setBlock(new BlockPos(x + dx, y, z + dz), y == -62 ? Blocks.DIRT.defaultBlockState() : y == -61 ? Blocks.GRASS_BLOCK.defaultBlockState() : Blocks.AIR.defaultBlockState(), 2 | 16);
                    if (set != null)
                        for (String p : set.split("/")) {
                            String[] kv = p.split("=");
                            String[] c = kv[0].split(",");
                            var blk = BuiltInRegistries.BLOCK.getValue(Identifier.parse(kv[1]));
                            level.setBlock(new BlockPos(x + Integer.parseInt(c[0]), -60 + Integer.parseInt(c[1]), z + Integer.parseInt(c[2])), blk.defaultBlockState(), 2 | 16);
                        }
                    var feat = (TreeFeature) features.getOrThrow(ResourceKey.create(Registries.FEATURE, Identifier.parse(a[0]))).value();
                    Set<BlockPos> ls = list(logs, x, z), lv = list(leaves, x, z), rs = list(roots, x, z);
                    for (BlockPos p : ls) level.setBlock(p, BuiltInRegistries.BLOCK.getValue(Identifier.parse("minecraft:spruce_log")).defaultBlockState(), 2 | 16);
                    for (BlockPos p : lv) level.setBlock(p, BuiltInRegistries.BLOCK.getValue(Identifier.parse("minecraft:oak_leaves")).defaultBlockState(), 2 | 16);
                    TreeDecorator d = feat.decorators().get(idx);
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    StringBuilder sb = new StringBuilder(line).append(" =>");
                    var ctx = new TreeDecorator.Context(level, (p, s) -> {
                        level.setBlock(p, s, 2 | 16);
                        sb.append(' ').append(p.getX() - x).append(',').append(p.getY()).append(',').append(p.getZ() - z).append('=').append(Draws.state(s));
                    }, r, new HashSet<>(ls), new HashSet<>(lv), new HashSet<>(rs));
                    d.place(ctx);
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
