package kremenmc.probe.structlegacy;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
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
import net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext;
import net.minecraft.world.level.storage.LevelStorageSource;

// Starts runs a structure's findGenerationPoint as a black box on the real
// overworld noise generator of a seed, with a start random that logs every
// draw, and prints the draws and the resulting pieces (createTag).
// Args: <seed> <structure> <x0> <z0> <x1> <z1> [log]. Chunks of the rect.
public final class Starts {
    static StringBuilder log;

    static final class LogWG extends WorldgenRandom {
        int depth;
        LogWG() { super(new LegacyRandomSource(0)); }
        <T> T l(String w, T v) { if (log != null && depth == 1) log.append(' ').append(w).append('=').append(v); return v; }
        public int nextInt(int b) { depth++; try { return l("i" + b, super.nextInt(b)); } finally { depth--; } }
        public int nextInt() { depth++; try { return l("i", super.nextInt()); } finally { depth--; } }
        public int nextInt(int a, int b) { depth++; try { return l("r" + a + "_" + b, super.nextInt(a, b)); } finally { depth--; } }
        public int nextIntBetweenInclusive(int a, int b) { depth++; try { return l("ib" + a + "_" + b, super.nextIntBetweenInclusive(a, b)); } finally { depth--; } }
        public long nextLong() { depth++; try { return l("l", super.nextLong()); } finally { depth--; } }
        public float nextFloat() { depth++; try { return l("f", super.nextFloat()); } finally { depth--; } }
        public double nextDouble() { depth++; try { return l("d", super.nextDouble()); } finally { depth--; } }
        public boolean nextBoolean() { depth++; try { return l("b", super.nextBoolean()); } finally { depth--; } }
    }

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
        String key = args[1];
        int x0 = Integer.parseInt(args[2]), z0 = Integer.parseInt(args[3]), x1 = Integer.parseInt(args[4]), z1 = Integer.parseInt(args[5]);
        boolean doLog = args.length > 6;
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                var ra = level.registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs = MultiNoiseBiomeSource.createFromPreset(preset);
                var gen0 = new NoiseBasedChunkGenerator(bs, settings);
                var gen = new LogGen(gen0);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                var holder = ra.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(key)));
                Structure st = holder.value();
                var fgp = Structure.class.getDeclaredMethod("findGenerationPoint", Structure.GenerationContext.class);
                fgp.setAccessible(true);
                var cs = rs.createClimateSampler(net.minecraft.world.level.levelgen.densityfunction.SamplerContext.EMPTY_UNCACHED);
                var sctx = StructurePieceSerializationContext.fromLevel(level);
                for (int cz = z0; cz <= z1; cz++) for (int cx = x0; cx <= x1; cx++) {
                    var r = new LogWG();
                    r.setLargeFeatureSeed(seed, cx, cz);
                    log = doLog ? new StringBuilder() : null;
                    gen.log = log;
                    var ctx = new Structure.GenerationContext(ra, gen, bs, cs, bs.createResolver(cs), rs, server.getStructureTemplateManager(), r, seed, new ChunkPos(cx, cz), level, b -> st.biomes().contains(b));
                    @SuppressWarnings("unchecked") var stub = (Optional<Structure.GenerationStub>) fgp.invoke(st, ctx);
                    StringBuilder sb = new StringBuilder(key).append(' ').append(cx).append(' ').append(cz);
                    if (stub.isPresent()) {
                        sb.append(ctx.isValidBiome(stub.get()) ? " valid" : " biome-rejected");
                        var pc = stub.get().getPiecesBuilder().build();
                        sb.append(" pos=").append(stub.get().position().toShortString().replace(" ", ""));
                        for (var p : pc.pieces()) sb.append(" PIECE ").append(p.createTag(sctx));
                        gen.log = null;
                        var bb = pc.calculateBoundingBox();
                        int mx = bb.minX() + (bb.maxX() - bb.minX() + 1) / 2, mz = bb.minZ() + (bb.maxZ() - bb.minZ() + 1) / 2;
                        int[][] pts = {{bb.minX(), bb.minZ()}, {bb.maxX(), bb.minZ()}, {bb.minX(), bb.maxZ()}, {bb.maxX(), bb.maxZ()}, {mx, mz}};
                        sb.append(" H");
                        for (int[] q : pts) sb.append(" ").append(q[0]).append(",").append(q[1]).append(":").append(gen.getBaseHeight(q[0], q[1], net.minecraft.world.level.levelgen.Heightmap.Types.WORLD_SURFACE_WG, level, rs)).append("/").append(gen.getBaseHeight(q[0], q[1], net.minecraft.world.level.levelgen.Heightmap.Types.OCEAN_FLOOR_WG, level, rs));
                    } else sb.append(" none");
                    if (log != null) sb.append(" | draws").append(log);
                    out.println(sb);
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
