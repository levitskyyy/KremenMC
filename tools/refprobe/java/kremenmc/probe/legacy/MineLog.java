package kremenmc.probe.legacy;

import java.io.PrintStream;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.Biomes;
import net.minecraft.world.level.biome.FixedBiomeSource;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.Structure;
import net.minecraft.world.level.levelgen.structure.StructurePiece;
import net.minecraft.world.level.levelgen.structure.pieces.StructurePiecesBuilder;

// MineLog runs a mineshaft's piece generation as a black box with a
// builder that logs every addPiece and findCollisionPiece (box and result)
// interleaved with the start random's draws (LOG=1).
// Args: <structure id> <seed> <cx> <cz>.
public final class MineLog {
    static PrintStream out;

    static String box(BoundingBox b) {
        return b.minX() + " " + b.minY() + " " + b.minZ() + " " + b.maxX() + " " + b.maxY() + " " + b.maxZ();
    }

    static final class LogBuilder extends StructurePiecesBuilder {
        public void addPiece(StructurePiece p) {
            out.println("ADD" + LogWR.nest() + " " + p.getClass().getSimpleName() + " " + box(p.getBoundingBox()) + " O=" + p.getOrientation() + " GD=" + p.getGenDepth());
            super.addPiece(p);
        }
        public StructurePiece findCollisionPiece(BoundingBox b) {
            StructurePiece r = super.findCollisionPiece(b);
            out.println("COL" + LogWR.nest() + " " + box(b) + " -> " + (r == null ? "none" : r.getClass().getSimpleName() + " " + box(r.getBoundingBox())));
            return r;
        }
    }

    public static void main(String[] args) throws Exception {
        out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        var lookup = VanillaRegistries.createWorldLookup();
        var s = lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse("minecraft:overworld")));
        var biome = lookup.lookupOrThrow(Registries.BIOME).getOrThrow(Biomes.PLAINS);
        var gen = new NoiseBasedChunkGenerator(new FixedBiomeSource(biome), s);
        Structure st = lookup.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(args[0]))).value();
        long seed = Long.parseLong(args[1]);
        var rs = RandomState.create(lookup.lookupOrThrow(Registries.NOISE), seed, s.value());
        int cx = Integer.parseInt(args[2]), cz = Integer.parseInt(args[3]);
        LogWR wr = new LogWR();
        LogWR.OUT = out;
        wr.setLargeFeatureSeed(seed, cx, cz);
        wr.on = System.getenv("LOG") != null;
        var ctx = new Structure.GenerationContext(null, gen, gen.getBiomeSource(), null, gen.getBiomeSource().createUncachedResolver(rs), rs, null, wr, seed,
            new ChunkPos(cx, cz), LevelHeightAccessor.create(-64, 384), h -> true);
        wr.nextDouble();
        var m = st.getClass().getDeclaredMethod("generatePiecesAndAdjust", StructurePiecesBuilder.class, Structure.GenerationContext.class);
        m.setAccessible(true);
        var b = new LogBuilder();
        Object y = m.invoke(st, b, ctx);
        out.println("RESULT " + y);
        for (StructurePiece p : b.build().pieces()) out.println("P " + p.getClass().getSimpleName() + " " + box(p.getBoundingBox()) + " O=" + p.getOrientation() + " GD=" + p.getGenDepth());
        out.println("NEXT " + wr.nextLong());
    }
}
