package kremenmc.probe.jigsaw;

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
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.structure.Structure;
import net.minecraft.world.level.storage.LevelStorageSource;

// JigStarts runs one structure's findGenerationPoint and builds its pieces
// (black box) with a start random that counts its bounded draws in the
// static field draws, so JigJdi can tag observed calls with the draw index.
// Args: <seed> <structure> <chunkX> <chunkZ>.
public final class JigStarts {
    public static volatile int draws;
    static final int LO = Integer.getInteger("kremenmc.jig.lo", -1), HI = Integer.getInteger("kremenmc.jig.hi", -1);

    // mark is a JigJdi breakpoint: on=true when draws reaches LO, false after HI.
    static void mark(boolean on) {}

    static final class CountWG extends WorldgenRandom {
        CountWG() { super(new LegacyRandomSource(0)); }
        public int nextInt(int b) {
            draws++;
            if (draws == LO) mark(true);
            if (draws == HI + 1) mark(false);
            return super.nextInt(b);
        }
    }

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "jigstartsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        String key = args[1];
        int cx = Integer.parseInt(args[2]), cz = Integer.parseInt(args[3]);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var level = server.overworld();
                var ra = level.registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs = MultiNoiseBiomeSource.createFromPreset(preset);
                var gen = new NoiseBasedChunkGenerator(bs, settings);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                Structure st = ra.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(key))).value();
                var fgp = Structure.class.getDeclaredMethod("findGenerationPoint", Structure.GenerationContext.class);
                fgp.setAccessible(true);
                var cs = rs.createClimateSampler(net.minecraft.world.level.levelgen.densityfunction.SamplerContext.EMPTY_UNCACHED);
                var r = new CountWG();
                r.setLargeFeatureSeed(seed, cx, cz);
                draws = 0;
                var ctx = new Structure.GenerationContext(ra, gen, bs, cs, bs.createResolver(cs), rs, server.getStructureTemplateManager(), r, seed, new ChunkPos(cx, cz), level, b -> st.biomes().contains(b));
                @SuppressWarnings("unchecked") var stub = (Optional<Structure.GenerationStub>) fgp.invoke(st, ctx);
                if (stub.isPresent()) out.println("pieces " + stub.get().getPiecesBuilder().build().pieces().size() + " draws " + draws);
                else out.println("none");
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
