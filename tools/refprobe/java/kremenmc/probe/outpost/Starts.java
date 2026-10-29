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
import net.minecraft.world.level.biome.BiomeResolver;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.structure.Structure;
import net.minecraft.world.level.storage.LevelStorageSource;

// Starts runs a jigsaw structure's findGenerationPoint (black box) at each
// chunk with a biome resolver that logs every quart query and a validBiome
// predicate that logs its answer, then prints the fresh pieces.
// Args: <seed> <structure> <cx> <cz> [<cx> <cz> ...].
public final class Starts {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "opstartsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        String key = args[1];
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var level = server.overworld();
                var ra = level.registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs0 = MultiNoiseBiomeSource.createFromPreset(preset);
                var gen = new NoiseBasedChunkGenerator(bs0, settings);
                var bs = new LogRes(bs0);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                Structure st = ra.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(key))).value();
                var fgp = Structure.class.getDeclaredMethod("findGenerationPoint", Structure.GenerationContext.class);
                fgp.setAccessible(true);
                var cs = rs.createClimateSampler(net.minecraft.world.level.levelgen.densityfunction.SamplerContext.EMPTY_UNCACHED);
                BiomeResolver base = bs.createResolver(cs);
                StringBuilder log = LogRes.log;
                BiomeResolver lr = (x, y, z) -> {
                    var b = base.getNoiseBiome(x, y, z);
                    log.append(" Q").append(x).append(',').append(y).append(',').append(z).append('=').append(b.unwrapKey().get().identifier().getPath());
                    return b;
                };
                for (int i = 2; i + 1 < args.length; i += 2) {
                    int cx = Integer.parseInt(args[i]), cz = Integer.parseInt(args[i + 1]);
                    log.setLength(0);
                    var r = new WorldgenRandom(new LegacyRandomSource(0));
                    r.setLargeFeatureSeed(seed, cx, cz);
                    var ctx = new Structure.GenerationContext(ra, gen, bs, cs, lr, rs, server.getStructureTemplateManager(), r, seed, new ChunkPos(cx, cz), level,
                        b -> { boolean ok = st.biomes().contains(b); log.append(" P=").append(ok);
                            if (Boolean.getBoolean("kremenmc.op.stack")) { var tr = Thread.currentThread().getStackTrace(); for (int k = 2; k < Math.min(tr.length, 9); k++) log.append(" @").append(tr[k].getClassName().replaceAll(".*\\.", "")).append('.').append(tr[k].getMethodName()); }
                            return ok; });
                    @SuppressWarnings("unchecked") var stub = Boolean.getBoolean("kremenmc.op.valid") ? st.findValidGenerationPoint(ctx) : (Optional<Structure.GenerationStub>) fgp.invoke(st, ctx);
                    if (stub.isPresent()) {
                        var ps = stub.get().getPiecesBuilder().build().pieces();
                        out.println("START " + cx + " " + cz + " pieces " + ps.size() + " stub " + stub.get().position().toShortString() + " LOG" + log);
                        for (var p : ps) {
                            var bb = p.getBoundingBox();
                            String s = "  BB " + bb.minX() + " " + bb.minY() + " " + bb.minZ() + " " + bb.maxX() + " " + bb.maxY() + " " + bb.maxZ();
                            if (p instanceof net.minecraft.world.level.levelgen.structure.PoolElementStructurePiece pe) {
                                s += " pos " + pe.getPosition().toShortString() + " rot " + pe.getRotation() + " el " + pe.getElement();
                            }
                            out.println(s);
                        }
                    } else out.println("NONE " + cx + " " + cz + " LOG" + log);
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
