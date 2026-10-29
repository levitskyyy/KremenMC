package kremenmc.probe.outpost;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.chunk.ChunkGeneratorStructureState;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.structure.placement.RandomSpreadStructurePlacement;
import net.minecraft.world.level.storage.LevelStorageSource;

// Excl samples the pillager outpost set's exclusion zone as a black box.
// For each outpost grid cell in [-R,R]: the potential chunk, whether the
// frequency test accepts it, isStructureChunk on the real overworld state
// and the public hasStructureChunkInRange(villages, x, z, 10).
// Args: <seed> <cell radius>.
public final class Excl {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "exclworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        int r = Integer.parseInt(args[1]);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var ra = server.overworld().registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs = MultiNoiseBiomeSource.createFromPreset(preset);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                var sets = ra.lookupOrThrow(Registries.STRUCTURE_SET);
                var state = ChunkGeneratorStructureState.createForNormal(rs, seed, new ChunkPos(0, 0), bs, sets);
                state.ensureStructuresGenerated();
                var op = sets.getOrThrow(ResourceKey.create(Registries.STRUCTURE_SET, Identifier.parse("minecraft:pillager_outposts")));
                var vil = sets.getOrThrow(ResourceKey.create(Registries.STRUCTURE_SET, Identifier.parse("minecraft:villages")));
                var p = (RandomSpreadStructurePlacement) op.value().placement();
                StringBuilder sb = new StringBuilder();
                for (int gz = -r; gz <= r; gz++) for (int gx = -r; gx <= r; gx++) {
                    ChunkPos c = p.getPotentialStructureChunk(seed, gx * p.spacing(), gz * p.spacing());
                    sb.append(c.x()).append(' ').append(c.z()).append(' ')
                        .append(p.applyAdditionalChunkRestrictions(c.x(), c.z(), seed) ? 1 : 0).append(' ')
                        .append(p.isStructureChunk(state, c.x(), c.z()) ? 1 : 0).append(' ')
                        .append(state.hasStructureChunkInRange(vil, c.x(), c.z(), 10) ? 1 : 0).append('\n');
                }
                out.print(sb);
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { out.flush(); Runtime.getRuntime().halt(0); }
    }
}
