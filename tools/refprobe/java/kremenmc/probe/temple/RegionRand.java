package kremenmc.probe.temple;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ChunkMap;
import net.minecraft.server.level.GenerationChunkHolder;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.WorldGenRegion;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.util.RandomSource;
import net.minecraft.util.StaticCache2D;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.chunk.status.ChunkPyramid;
import net.minecraft.world.level.chunk.status.ChunkStatus;
import net.minecraft.world.level.storage.LevelStorageSource;

// RegionRand builds real WorldGenRegions (features step) around loaded
// chunks of the GameTest world and prints the first draws of getRandom(),
// next to candidate constructions from the level's RandomState, to identify
// the region random black-box. Args: cx,cz ...
public final class RegionRand {
    static String longs(RandomSource r) {
        StringBuilder b = new StringBuilder();
        for (int i = 0; i < 3; i++) b.append(' ').append(r.nextLong());
        return b.toString();
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "regionrandworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                ChunkMap cm = level.getChunkSource().chunkMap;
                var gv = ChunkMap.class.getDeclaredMethod("getVisibleChunkIfPresent", long.class);
                gv.setAccessible(true);
                var idf = WorldGenRegion.class.getDeclaredField("WORLDGEN_REGION_RANDOM");
                idf.setAccessible(true);
                Object id = idf.get(null);
                var rs = level.getChunkSource().randomState();
                out.println("SEED " + level.getSeed() + " ID " + id);
                var step = ChunkPyramid.GENERATION_PYRAMID.getStepTo(ChunkStatus.FEATURES);
                for (String a : args) {
                    String[] p = a.split(",");
                    int cx = Integer.parseInt(p[0]), cz = Integer.parseInt(p[1]);
                    for (int dx = -2; dx <= 2; dx++) for (int dz = -2; dz <= 2; dz++) level.getChunk(cx + dx, cz + dz);
                    StaticCache2D<GenerationChunkHolder> cache = StaticCache2D.create(cx, cz, 1, (x, z) -> {
                        try { return (GenerationChunkHolder) gv.invoke(cm, ChunkPos.pack(x, z)); } catch (Exception e) { throw new RuntimeException(e); }
                    });
                    var region = new WorldGenRegion(level, cache, step, level.getChunk(cx, cz));
                    out.println("REGION " + cx + " " + cz + longs(region.getRandom()));
                    var fac = rs.getOrCreateRandomFactory((net.minecraft.resources.Identifier) id);
                    out.println("HYP_at_world " + cx + " " + cz + longs(fac.at(cx << 4, 0, cz << 4)));
                    out.println("HYP_at_000 " + cx + " " + cz + longs(fac.at(0, 0, 0)));
                    var region2 = new WorldGenRegion(level, cache, step, level.getChunk(cx, cz));
                    out.println("REGION2 " + cx + " " + cz + longs(region2.getRandom()));
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { out.flush(); Runtime.getRuntime().halt(0); }
    }
}
