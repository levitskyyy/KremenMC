package kremenmc.probe.structlegacy;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.chunk.ChunkGeneratorStructureState;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.structure.placement.ConcentricRingsStructurePlacement;
import net.minecraft.world.level.storage.LevelStorageSource;

// Rings prints, as a black box, the concentric_rings positions of every
// such structure set for a seed on the real overworld biome source.
// Args: <seed> [originX originZ].
public final class Rings {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "structstartsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        ChunkPos origin = args.length > 2 ? new ChunkPos(Integer.parseInt(args[1]), Integer.parseInt(args[2])) : new ChunkPos(0, 0);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var ra = server.overworld().registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs0 = MultiNoiseBiomeSource.createFromPreset(preset);
                var lbs = new LogBS(bs0);
                var bs = args.length > 3 ? lbs : bs0;
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                var sets = ra.lookupOrThrow(Registries.STRUCTURE_SET);
                var state = ChunkGeneratorStructureState.createForNormal(rs, seed, origin, bs, sets);
                state.ensureStructuresGenerated();
                sets.listElements().forEach(h -> {
                    if (h.value().placement() instanceof ConcentricRingsStructurePlacement p) {
                        StringBuilder sb = new StringBuilder(h.key().identifier().toString());
                        for (ChunkPos c : state.getRingPositionsFor(p)) sb.append(' ').append(c.x()).append(',').append(c.z());
                        out.println(sb);
                    }
                });
                out.print(lbs.log);
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { out.flush(); Runtime.getRuntime().halt(0); }
    }
}
