package kremenmc.probe.nstruct;

import java.util.List;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Holder;
import net.minecraft.server.level.WorldGenRegion;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.NoiseColumn;
import net.minecraft.world.level.StructureManager;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeManager;
import net.minecraft.world.level.chunk.ChunkAccess;
import net.minecraft.world.level.chunk.ChunkGenerator;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.blending.Blender;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;

// LogGen forwards to a chunk generator and logs every base height and base
// column query (which columns a structure start samples).
final class NLogGen extends ChunkGenerator {
    final ChunkGenerator d;
    StringBuilder log;

    NLogGen(ChunkGenerator d) { super(d.getBiomeSource()); this.d = d; }

    protected com.mojang.serialization.MapCodec<? extends ChunkGenerator> codec() { return null; }
    public void spawnOriginalMobs(WorldGenRegion r) {}
    public int getGenDepth() { return d.getGenDepth(); }
    public CompletableFuture<ChunkAccess> buildTerrain(ChunkAccess c, Blender b, RandomState rs, StructureManager sm, BiomeManager bm, WorldGenRegion r, Set<Holder<Biome>> s) { return d.buildTerrain(c, b, rs, sm, bm, r, s); }
    public int getSeaLevel() { return d.getSeaLevel(); }
    public int getMinY() { return d.getMinY(); }
    public int getBaseHeight(int x, int z, Heightmap.Types t, LevelHeightAccessor h, RandomState rs) {
        int v = d.getBaseHeight(x, z, t, h, rs);
        if (log != null) log.append(" BH(").append(x).append(',').append(z).append(',').append(t).append(")=").append(v);
        return v;
    }
    public NoiseColumn getBaseColumn(int x, int z, LevelHeightAccessor h, RandomState rs) {
        if (log != null) log.append(" BC(").append(x).append(',').append(z).append(')');
        return d.getBaseColumn(x, z, h, rs);
    }
    public void addDebugScreenInfo(List<String> l, RandomState rs, BlockPos p, SamplerContext c) {}
}
