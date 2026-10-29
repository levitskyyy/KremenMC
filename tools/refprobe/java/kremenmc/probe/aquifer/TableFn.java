package kremenmc.probe.aquifer;

import com.mojang.serialization.MapCodec;
import java.util.HashMap;
import java.util.Map;
import net.minecraft.util.Interval;
import net.minecraft.world.level.levelgen.densityfunction.DensityBuffer;
import net.minecraft.world.level.levelgen.densityfunction.DensityFunction;
import net.minecraft.world.level.levelgen.densityfunction.DensitySampler;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.densityfunction.DfRewriteRule;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;

// A density function read from a table set by the driver, keyed by chunk
// (mode c), column (xz) or block (p); missing keys give the default.
record TableFn(String name, String mode, float def) implements DensityFunction {
    static final Map<String, Map<String, Float>> tables = new HashMap<>();

    static String key(String mode, int x, int y, int z) {
        return switch (mode) {
            case "c" -> (x >> 4) + "," + (z >> 4);
            case "xz" -> x + "," + z;
            default -> x + "," + y + "," + z;
        };
    }

    float value(int x, int y, int z) {
        Map<String, Float> t = tables.get(name);
        if (t == null) return def;
        return t.getOrDefault(key(mode, x, y, z), def);
    }

    @Override
    public DensitySampler compileSampler(CompileContext ctx) {
        return new DensitySampler() {
            @Override
            public void sampleVolume(SamplerContext c, DensityBuffer b, DensityVolume v) {
                DensitySampler.sampleVolumeNaive(c, b, v, this);
            }

            @Override
            public float sampleValue(SamplerContext c, int x, int y, int z) { return value(x, y, z); }
        };
    }

    @Override
    public DensityFunction rewriteChildren(DfRewriteRule rule) { return this; }

    @Override
    public Interval range() { return Interval.INFINITE; }

    @Override
    public int domainAxes() { return DensityFunction.ALL_AXES; }

    @Override
    public MapCodec<? extends DensityFunction> codec() { return null; }
}
