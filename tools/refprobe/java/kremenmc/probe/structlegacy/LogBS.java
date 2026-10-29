package kremenmc.probe.structlegacy;

import java.util.function.Predicate;
import java.util.stream.Stream;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Holder;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeResolver;
import net.minecraft.world.level.biome.BiomeSource;
import net.minecraft.world.level.biome.Climate;
import net.minecraft.world.level.levelgen.RandomState;

// LogBS forwards to a biome source and logs findBiomeHorizontal calls
// (arguments, the random's state before and after, result).
final class LogBS extends BiomeSource {
    final BiomeSource d;
    final StringBuilder log = new StringBuilder();

    LogBS(BiomeSource d) { this.d = d; }

    protected com.mojang.serialization.MapCodec<? extends BiomeSource> codec() { return null; }
    protected Stream<Holder<Biome>> collectPossibleBiomes() { return d.possibleBiomes().stream(); }
    public BiomeResolver createResolver(Climate.Sampler s) { return d.createResolver(s); }

    public com.mojang.datafixers.util.Pair<BlockPos, Holder<Biome>> findBiomeHorizontal(int x, int y, int z, int r, int step, Predicate<Holder<Biome>> p, RandomSource rnd, boolean first, RandomState rs) {
        StringBuilder dl = new StringBuilder();
        RandomSource lr = new RandomSource() {
            public RandomSource fork() { return rnd.fork(); }
            public net.minecraft.world.level.levelgen.PositionalRandomFactory forkPositional() { return rnd.forkPositional(); }
            public void setSeed(long s) { rnd.setSeed(s); }
            public int nextInt() { int v = rnd.nextInt(); dl.append(" i=").append(v); return v; }
            public int nextInt(int b) { int v = rnd.nextInt(b); dl.append(" i").append(b).append("=").append(v); return v; }
            public long nextLong() { long v = rnd.nextLong(); dl.append(" l=").append(v); return v; }
            public boolean nextBoolean() { boolean v = rnd.nextBoolean(); dl.append(" b=").append(v); return v; }
            public float nextFloat() { float v = rnd.nextFloat(); dl.append(" f=").append(v); return v; }
            public double nextDouble() { double v = rnd.nextDouble(); dl.append(" d=").append(v); return v; }
            public double nextGaussian() { double v = rnd.nextGaussian(); dl.append(" g=").append(v); return v; }
        };
        String st0 = "?";
        try { var sf = net.minecraft.world.level.levelgen.LegacyRandomSource.class.getDeclaredField("seed"); sf.setAccessible(true); st0 = String.valueOf(((java.util.concurrent.atomic.AtomicLong) sf.get(rnd)).get()); } catch (Exception e) { st0 = e.toString(); }
        String cls = rnd.getClass().getSimpleName() + "@" + System.identityHashCode(rnd) + " state=" + st0;
        var res = d.findBiomeHorizontal(x, y, z, r, step, p, lr, first, rs);
        synchronized (log) {
            log.append("FBH ").append(x).append(',').append(y).append(',').append(z).append(" r=").append(r).append(" step=").append(step).append(" first=").append(first)
                .append(" rnd=").append(cls).append(" -> ").append(res == null ? "null" : res.getFirst().getX() + "," + res.getFirst().getZ()).append(" draws").append(dl).append('\n');
        }
        return res;
    }
}
