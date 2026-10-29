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
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.structure.Structure;
import net.minecraft.world.level.levelgen.structure.StructurePiece;

// Starts runs one structure's findGenerationPoint at a chunk as a black box
// with every draw of the start random logged (LOG=1), then prints each
// piece's class, box, orientation and genDepth.
// Args: <structure id> <seed> <cx> <cz>.
public final class Starts {
    public static void main(String[] args) throws Exception {
        PrintStream out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        var lookup = VanillaRegistries.createWorldLookup();
        var s = lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse("minecraft:overworld")));
        var biome = lookup.lookupOrThrow(Registries.BIOME).getOrThrow(Biomes.PLAINS);
        var gen = new NoiseBasedChunkGenerator(new FixedBiomeSource(biome), s);
        Structure st = lookup.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(args[0]))).value();
        long seed = Long.parseLong(args[1]);
        var tmp = java.nio.file.Files.createTempDirectory("kremenmc-legacy");
        var access = net.minecraft.world.level.storage.LevelStorageSource.createDefault(tmp).createAccess("w");
        var rm = net.minecraft.server.packs.repository.ServerPacksSource.createVanillaPackSource().asResourceManager();
        var stm = new net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplateManager(rm, access,
            net.minecraft.util.datafix.DataFixers.getDataFixer(), net.minecraft.core.registries.BuiltInRegistries.BLOCK);
        var rs = RandomState.create(lookup.lookupOrThrow(Registries.NOISE), seed, s.value());
        for (int ai = 2; ai + 1 < args.length; ai += 2) {
        int cx = Integer.parseInt(args[ai]), cz = Integer.parseInt(args[ai + 1]);
        out.println("START " + cx + " " + cz);
        LogWR wr = new LogWR();
        LogWR.OUT = out;
        wr.setLargeFeatureSeed(seed, cx, cz);
        wr.on = System.getenv("LOG") != null;
        var ctx = new Structure.GenerationContext(null, gen, gen.getBiomeSource(), null, gen.getBiomeSource().createUncachedResolver(rs), rs, stm, wr, seed,
            new ChunkPos(cx, cz), LevelHeightAccessor.create(-64, 384), h -> true);
        java.lang.reflect.Method fm = Structure.class.getDeclaredMethod("findGenerationPoint", Structure.GenerationContext.class);
        fm.setAccessible(true);
        @SuppressWarnings("unchecked") var stub = (java.util.Optional<Structure.GenerationStub>) fm.invoke(st, ctx);
        if (stub.isEmpty()) { out.println("NONE"); continue; }
        out.println("STUB " + stub.get().position());
        var pb = stub.get().getPiecesBuilder();
        for (StructurePiece p : pb.build().pieces()) {
            var b = p.getBoundingBox();
            out.println("P " + p.getClass().getSimpleName() + " " + b.minX() + " " + b.minY() + " " + b.minZ() + " " + b.maxX() + " " + b.maxY() + " " + b.maxZ()
                + " O=" + p.getOrientation() + " GD=" + p.getGenDepth()
                + (p instanceof net.minecraft.world.level.levelgen.structure.TemplateStructurePiece tp ? " TP=" + tp.templatePosition() + " rot=" + tp.getRotation() : ""));
        }
        out.println("NEXT " + wr.nextLong());
        }
    }
}
