package kremenmc.probe.surface;

import java.util.HashSet;
import java.util.Set;
import java.util.function.Supplier;
import net.minecraft.core.Holder;
import net.minecraft.core.RegistryAccess;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeManager;
import net.minecraft.world.level.biome.BiomeResolver;
import net.minecraft.world.level.biome.BiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.biome.TheEndBiomeSource;
import net.minecraft.world.level.chunk.PalettedContainerFactory;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.chunk.UpgradeData;
import net.minecraft.world.level.levelgen.Aquifer;
import net.minecraft.world.level.levelgen.Beardifier;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.NoiseSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldGenerationContext;
import net.minecraft.world.level.levelgen.blending.Blender;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.material.rule.MaterialRule;

// Gen reproduces the TERRAIN status' noise fill and material pass for one
// chunk outside a world: biomes from the dimension's biome source, noise fill
// through the generator's own (private) code, then MaterialSystem.buildSurface
// with any rule.
final class Gen {
    final RegistryAccess ra;
    final Holder<NoiseGeneratorSettings> settings;
    final NoiseSettings ns;
    final BiomeSource src;
    final NoiseBasedChunkGenerator gen;
    final RandomState rs;
    final long seed;
    final BiomeResolver resolver;

    Gen(String settingsKey, String dim, long seed) {
        ra = Env.server.registryAccess();
        this.seed = seed;
        settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse(settingsKey)));
        ns = settings.value().noiseSettings();
        var lists = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST);
        src = switch (dim) {
            case "overworld" -> MultiNoiseBiomeSource.createFromPreset(lists.getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD));
            case "the_nether" -> MultiNoiseBiomeSource.createFromPreset(lists.getOrThrow(MultiNoiseBiomeSourceParameterLists.NETHER));
            default -> TheEndBiomeSource.create(ra.lookupOrThrow(Registries.BIOME));
        };
        gen = new NoiseBasedChunkGenerator(src, settings);
        rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
        resolver = src.createUncachedResolver(rs);
    }

    int minY() { return ns.minY(); }
    int height() { return ns.height(); }

    // Biome lookups clamp the quart y to the build range like ChunkAccess.getNoiseBiome.
    BiomeResolver clamped() {
        int minQ = minY() >> 2, maxQ = ((minY() + height()) >> 2) - 1;
        return (x, y, z) -> resolver.getNoiseBiome(x, Math.max(minQ, Math.min(maxQ, y)), z);
    }

    // Biome lookups read stored biomes of chunks filled by the generator's
    // own BIOMES step, like WorldGenRegion.
    BiomeManager biomeManager() {
        return new BiomeManager((x, y, z) -> biomeChunk(x >> 2, z >> 2).getNoiseBiome(x, y, z), BiomeManager.obfuscateSeed(seed));
    }

    final java.util.Map<Long, ProtoChunk> biomeChunks = new java.util.HashMap<>();

    ProtoChunk biomeChunk(int cx, int cz) {
        long k = ChunkPos.pack(cx, cz);
        ProtoChunk pc = biomeChunks.get(k);
        if (pc == null) {
            if (biomeChunks.size() > 64) biomeChunks.clear();
            pc = new ProtoChunk(new ChunkPos(cx, cz), UpgradeData.EMPTY, LevelHeightAccessor.create(minY(), height()), PalettedContainerFactory.create(ra), null);
            gen.createBiomes(rs, Blender.empty(), null, pc).join();
            pc.setPersistedStatus(net.minecraft.world.level.chunk.status.ChunkStatus.BIOMES);
            biomeChunks.put(k, pc);
        }
        return pc;
    }

    ProtoChunk newChunk(int cx, int cz) {
        ProtoChunk pc = new ProtoChunk(new ChunkPos(cx, cz), UpgradeData.EMPTY, LevelHeightAccessor.create(minY(), height()), PalettedContainerFactory.create(ra), null);
        gen.createBiomes(rs, Blender.empty(), null, pc).join();
        pc.setPersistedStatus(net.minecraft.world.level.chunk.status.ChunkStatus.BIOMES);
        return pc;
    }

    @SuppressWarnings("unchecked")
    NoiseChunk noiseChunk(ProtoChunk pc) throws Exception {
        DensityVolume vol = (DensityVolume) Env.method(NoiseBasedChunkGenerator.class, "chunkVolume").invoke(null, pc, ns);
        Aquifer.FluidPicker fp = ((Supplier<Aquifer.FluidPicker>) Env.get(gen, NoiseBasedChunkGenerator.class, "globalFluidPicker")).get();
        return new NoiseChunk(rs, Beardifier.EMPTY, settings.value(), fp, Blender.empty(), vol);
    }

    void fill(NoiseChunk nc, ProtoChunk pc) throws Exception {
        Env.method(NoiseBasedChunkGenerator.class, "doFill").invoke(gen, nc, pc);
    }

    Set<Holder<Biome>> chunkBiomes(ProtoChunk pc) {
        Set<Holder<Biome>> s = new HashSet<>();
        pc.collectBiomesInPalette(s);
        return s;
    }

    void surface(NoiseChunk nc, ProtoChunk pc, MaterialRule rule, Set<Holder<Biome>> possible) {
        rs.surfaceSystem().buildSurface(rs, biomeManager(), new WorldGenerationContext(gen, pc), pc, nc, rule, possible);
    }
}
