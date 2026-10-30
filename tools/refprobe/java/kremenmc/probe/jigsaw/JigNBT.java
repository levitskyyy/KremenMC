package kremenmc.probe.jigsaw;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.regex.Pattern;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.structure.PoolElementStructurePiece;
import net.minecraft.world.level.levelgen.structure.Structure;
import net.minecraft.world.level.storage.LevelStorageSource;

// JigNBT runs a jigsaw structure's findValidGenerationPoint (black box) at
// each chunk and prints the fresh pieces as "loc x,y,z ROT [bb] gld=G J=..."
// (the layout of jigsaw_ref_test refPieceKey), plus the start random's
// bounded draw count.
// Args: <seed> <overworld|the_nether> (<structure> <cx> <cz> [<cx> <cz> ...])...
public final class JigNBT {
    static int draws;
    static PrintStream OUT;

    static final class CountWG extends WorldgenRandom {
        CountWG() { super(new LegacyRandomSource(0)); }
        static final int STACK = Integer.getInteger("kremenmc.jig.stack", 0);
        int calls;
        void trace(String what) {
            if (calls++ >= STACK) return;
            var tr = Thread.currentThread().getStackTrace();
            StringBuilder b = new StringBuilder("  TRACE " + what);
            for (int k = 3; k < Math.min(tr.length, 12); k++) b.append(" @").append(tr[k].getClassName().replaceAll(".*\\.", "")).append('.').append(tr[k].getMethodName());
            OUT.println(b);
        }
        public int next(int bits) { trace("next" + bits); return super.next(bits); }
        public net.minecraft.util.RandomSource fork() { trace("fork"); return super.fork(); }
        public net.minecraft.world.level.levelgen.PositionalRandomFactory forkPositional() { trace("forkPositional"); return super.forkPositional(); }
        public int nextInt(int b) { draws++; return super.nextInt(b); }
        public int nextInt(int a, int b) { draws++; return super.nextInt(a, b); }
    }

    static final Pattern LOC = Pattern.compile("(?:location|feature):\"?([a-z0-9_:/.-]+)");

    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        OUT = out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "jignbtworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        boolean nether = args[1].equals("the_nether");
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var level = nether ? server.getLevel(Level.NETHER) : server.overworld();
                var ra = level.registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(nether ? NoiseGeneratorSettings.NETHER : NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(nether ? MultiNoiseBiomeSourceParameterLists.NETHER : MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs = MultiNoiseBiomeSource.createFromPreset(preset);
                var gen = new NoiseBasedChunkGenerator(bs, settings);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                Structure st = null;
                var cs = rs.createClimateSampler(net.minecraft.world.level.levelgen.densityfunction.SamplerContext.EMPTY_UNCACHED);
                var sc = net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext.fromLevel(level);
                for (int i = 2; i < args.length; i += 2) {
                    if (args[i].contains(":")) {
                        st = ra.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(args[i]))).value();
                        out.println("STRUCTURE " + args[i]);
                        i--;
                        continue;
                    }
                    final Structure fst = st;
                    int cx = Integer.parseInt(args[i]), cz = Integer.parseInt(args[i + 1]);
                    var r = new CountWG();
                    r.setLargeFeatureSeed(seed, cx, cz);
                    draws = 0;
                    var ctx = new Structure.GenerationContext(ra, gen, bs, cs, bs.createResolver(cs), rs, server.getStructureTemplateManager(), r, seed, new ChunkPos(cx, cz), level, b -> fst.biomes().contains(b));
                    var stub = fst.findValidGenerationPoint(ctx);
                    if (stub.isEmpty()) {
                        out.println("NONE " + cx + " " + cz + " draws " + draws);
                        continue;
                    }
                    var ps = stub.get().getPiecesBuilder().build().pieces();
                    out.println("START " + cx + " " + cz + " pieces " + ps.size() + " draws " + draws);
                    for (var p : ps) {
                        var bb = p.getBoundingBox();
                        String box = "[" + bb.minX() + " " + bb.minY() + " " + bb.minZ() + " " + bb.maxX() + " " + bb.maxY() + " " + bb.maxZ() + "]";
                        if (!(p instanceof PoolElementStructurePiece pe)) {
                            out.println("  " + p.getClass().getSimpleName() + " " + box);
                            continue;
                        }
                        String el = p.createTag(sc).get("pool_element").toString();
                        var m = LOC.matcher(el);
                        String loc = m.find() ? m.group(1) : el;
                        StringBuilder js = new StringBuilder();
                        for (var j : pe.getJunctions()) {
                            if (js.length() > 0) js.append(' ');
                            js.append(j.getSourceX()).append(',').append(j.getSourceGroundY()).append(',').append(j.getSourceZ()).append(',')
                                .append(j.getDeltaY()).append(',').append(j.getDestProjection().toString().toLowerCase());
                        }
                        var pos = pe.getPosition();
                        out.println("  " + loc + " " + pos.getX() + "," + pos.getY() + "," + pos.getZ() + " " + pe.getRotation() + " " + box + " gld=" + pe.getGroundLevelDelta() + " J=" + js);
                        if (Boolean.getBoolean("kremenmc.jig.el")) out.println("    EL " + el);
                    }
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
