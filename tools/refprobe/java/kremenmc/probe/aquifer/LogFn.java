package kremenmc.probe.aquifer;

import com.mojang.serialization.MapCodec;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.util.Interval;
import net.minecraft.world.level.levelgen.densityfunction.DensityBuffer;
import net.minecraft.world.level.levelgen.densityfunction.DensityFunction;
import net.minecraft.world.level.levelgen.densityfunction.DensitySampler;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.densityfunction.DfRewriteRule;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;

// A density function that records every point it is sampled at.
record LogFn(String name, DensityFunction inner) implements DensityFunction {
    static final List<String> log = new ArrayList<>();

    @Override
    public DensitySampler compileSampler(CompileContext ctx) {
        DensitySampler s = inner.compileSampler(ctx);
        return new DensitySampler() {
            @Override
            public void sampleVolume(SamplerContext c, DensityBuffer b, DensityVolume v) {
                log.add(name + ":vol:" + v);
                DensitySampler.sampleVolumeNaive(c, b, v, this);
            }

            @Override
            public float sampleValue(SamplerContext c, int x, int y, int z) {
                log.add(name + ":" + x + "," + y + "," + z);
                return s.sampleValue(c, x, y, z);
            }
        };
    }

    @Override
    public DensityFunction rewriteChildren(DfRewriteRule rule) { return this; }

    @Override
    public Interval range() { return inner.range(); }

    @Override
    public int domainAxes() { return DensityFunction.ALL_AXES; }

    @Override
    public MapCodec<? extends DensityFunction> codec() { return inner.codec(); }
}
