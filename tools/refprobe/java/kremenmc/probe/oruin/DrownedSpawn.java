package kremenmc.probe.oruin;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ChunkMap;
import net.minecraft.server.level.GenerationChunkHolder;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.WorldGenRegion;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.util.StaticCache2D;
import net.minecraft.world.entity.EntitySpawnReason;

import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.chunk.status.ChunkPyramid;
import net.minecraft.world.level.chunk.status.ChunkStatus;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.storage.LevelStorageSource;

// DrownedSpawn: (1) builds a real WorldGenRegion (features step) and prints
// its getCurrentDifficultyAt and getSeaLevel; (2) runs n times what an
// ocean ruin "drowned" marker does to a fresh drowned (finalizeSpawn with
// reason STRUCTURE through a proxy level whose getRandom is a logging
// xoroshiro(seed i), difficulty from the region; then moveTo, persistence)
// and prints every draw ("Q") and the NBT of the drowned and its vehicle /
// passengers. Args: n [difficultyFromServerLevel=0|1]
public final class DrownedSpawn {
    public static void main(String[] args) throws Exception {
        PrintStream out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        LogWR.OUT = out;
        LogWR.LIMIT = 3;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "drownedworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        int n = Integer.parseInt(args[0]);
        boolean fromServer = args.length > 1 && args[1].equals("1");
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                ChunkMap cm = level.getChunkSource().chunkMap;
                var gv = ChunkMap.class.getDeclaredMethod("getVisibleChunkIfPresent", long.class);
                gv.setAccessible(true);
                var step = ChunkPyramid.GENERATION_PYRAMID.getStepTo(ChunkStatus.FEATURES);
                for (int dx = -2; dx <= 2; dx++) for (int dz = -2; dz <= 2; dz++) level.getChunk(dx, dz);
                StaticCache2D<GenerationChunkHolder> cache = StaticCache2D.create(0, 0, 1, (x, z) -> {
                    try { return (GenerationChunkHolder) gv.invoke(cm, ChunkPos.pack(x, z)); } catch (Exception e) { throw new RuntimeException(e); }
                });
                var region = new WorldGenRegion(level, cache, step, level.getChunk(0, 0));
                BlockPos at = new BlockPos(5, -50, 5);
                var rd = region.getCurrentDifficultyAt(at);
                var sd = level.getCurrentDifficultyAt(at);
                out.println("REGION difficulty " + rd.getDifficulty() + " effective " + rd.getEffectiveDifficulty() + " special " + rd.getSpecialMultiplier() + " sea " + region.getSeaLevel());
                out.println("SERVER difficulty " + sd.getDifficulty() + " effective " + sd.getEffectiveDifficulty() + " special " + sd.getSpecialMultiplier() + " sea " + level.getSeaLevel());
                var diff = fromServer ? sd : rd;
                for (int i = 0; i < n; i++) {
                    final LogRS rr = new LogRS(new XoroshiroRandomSource(i));
                    rr.on = true;
                    final ServerLevel lvl = level;
                    net.minecraft.world.level.ServerLevelAccessor wl = (net.minecraft.world.level.ServerLevelAccessor) java.lang.reflect.Proxy.newProxyInstance(DrownedSpawn.class.getClassLoader(),
                        new Class<?>[]{net.minecraft.world.level.ServerLevelAccessor.class}, (proxy, m, margs) -> {
                            if (m.getName().equals("getRandom")) return rr;
                            return m.invoke(lvl, margs);
                        });
                    out.println("TRIAL " + i);
                    var d = net.minecraft.world.entity.EntityTypes.DROWNED.create(level, EntitySpawnReason.STRUCTURE);
                    d.setPersistenceRequired();
                    d.snapTo(at, 0.0F, 0.0F);
                    d.finalizeSpawn(wl, diff, EntitySpawnReason.STRUCTURE, null);
                    out.println("ENT " + nbt(level, d));
                    var v = d.getVehicle();
                    if (v != null) out.println("VEHICLE " + BuiltInRegistries.ENTITY_TYPE.getKey(v.getType()) + " " + nbt(level, v));
                    for (var p : d.getPassengers()) out.println("PASSENGER " + BuiltInRegistries.ENTITY_TYPE.getKey(p.getType()) + " " + nbt(level, p));
                    out.println("NEXT " + rr.d.nextLong());
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { out.flush(); Runtime.getRuntime().halt(0); }
    }

    static String nbt(ServerLevel level, net.minecraft.world.entity.Entity e) {
        var to = net.minecraft.world.level.storage.TagValueOutput.createWithContext(net.minecraft.util.ProblemReporter.DISCARDING, level.registryAccess());
        e.saveWithoutId(to);
        CompoundTag et = to.buildResult();
        for (String drop : new String[]{"UUID", "Motion", "FallDistance", "Fire", "Air", "OnGround", "PortalCooldown", "HurtTime", "DeathTime", "HurtByTimestamp", "AbsorptionAmount", "FallFlying", "Brain", "fall_distance", "Pos", "current_impulse_context_reset_grace_time"}) et.remove(drop);
        return et.toString();
    }
}
