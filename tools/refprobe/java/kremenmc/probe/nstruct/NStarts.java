package kremenmc.probe.nstruct;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.StructureManager;
import net.minecraft.world.level.biome.BiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.biome.TheEndBiomeSource;
import net.minecraft.world.level.chunk.PalettedContainerFactory;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.chunk.UpgradeData;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldOptions;
import net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext;
import net.minecraft.world.level.storage.LevelStorageSource;

// NStarts runs the real STRUCTURE_STARTS step (ChunkGenerator.createStructures
// with the dimension's structure state) on fresh proto chunks and prints every
// start: structure key, chunk, and with FULL=1 the start SNBT.
// Args: <seed> <overworld|the_nether|the_end> <cx0> <cz0> <cx1> <cz1> [step]
public final class NStarts {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "nstartsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        String dim = args[1];
        int cx0 = Integer.parseInt(args[2]), cz0 = Integer.parseInt(args[3]), cx1 = Integer.parseInt(args[4]), cz1 = Integer.parseInt(args[5]);
        int step = args.length > 6 ? Integer.parseInt(args[6]) : 1;
        boolean full = "1".equals(System.getenv("FULL"));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                var ra = level.registryAccess();
                var lists = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST);
                BiomeSource bs;
                net.minecraft.resources.ResourceKey<NoiseGeneratorSettings> sk;
                net.minecraft.resources.ResourceKey<Level> lk;
                switch (dim) {
                    case "the_nether" -> { bs = MultiNoiseBiomeSource.createFromPreset(lists.getOrThrow(MultiNoiseBiomeSourceParameterLists.NETHER)); sk = NoiseGeneratorSettings.NETHER; lk = Level.NETHER; }
                    case "the_end" -> { bs = TheEndBiomeSource.create(ra.lookupOrThrow(Registries.BIOME)); sk = NoiseGeneratorSettings.END; lk = Level.END; }
                    default -> { bs = MultiNoiseBiomeSource.createFromPreset(lists.getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD)); sk = NoiseGeneratorSettings.OVERWORLD; lk = Level.OVERWORLD; }
                }
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(sk);
                var gen = new NoiseBasedChunkGenerator(bs, settings);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                var state = gen.createState(ra.lookupOrThrow(Registries.STRUCTURE_SET), rs, seed);
                StringBuilder sets = new StringBuilder("SETS");
                for (var h : state.possibleStructureSets()) sets.append(' ').append(h.unwrapKey().map(k -> k.identifier().toString()).orElse("?"));
                out.println(sets);
                var sm = level.structureManager();
                var ns = settings.value().noiseSettings();
                var sctx = StructurePieceSerializationContext.fromLevel(level);
                var sreg = ra.lookupOrThrow(Registries.STRUCTURE);
                for (int cz = cz0; cz <= cz1; cz += step) {
                    for (int cx = cx0; cx <= cx1; cx += step) {
                        var pc = new ProtoChunk(new ChunkPos(cx, cz), UpgradeData.EMPTY, LevelHeightAccessor.create(ns.minY(), ns.height()), PalettedContainerFactory.create(ra), null);
                        gen.createStructures(ra, state, sm, pc, server.getStructureTemplateManager(), lk);
                        for (var e : pc.getAllStarts().entrySet()) {
                            var st = e.getValue();
                            if (!st.isValid()) continue;
                            StringBuilder sb = new StringBuilder("START ").append(sreg.getKey(e.getKey())).append(' ').append(cx).append(' ').append(cz).append(" pieces=").append(st.getPieces().size()).append(" bb=").append(st.getBoundingBox());
                            if (full) sb.append(" NBT ").append(st.createTag(sctx, new ChunkPos(cx, cz)));
                            out.println(sb);
                        }
                    }
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
