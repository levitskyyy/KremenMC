package kremenmc.probe.noise;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.util.Random;
import net.minecraft.SharedConstants;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.densityfunction.DensitySampler;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;
import net.minecraft.world.level.levelgen.synth.BlendedNoise;

// BlendedNoise: createFbm structure, FbmSet structure, range and sampled
// values of the compiled sampler at block positions.
// args: <seed> <instances> <samplesPerInstance>
public final class BlendedProbe {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Random r = new Random(Long.parseLong(args[0]));
        int inst = Integer.parseInt(args[1]), n = Integer.parseInt(args[2]);
        PrintStream o = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false);
        for (int i = 0; i < 12; i++) {
            long sd = r.nextLong();
            int first = -r.nextInt(16);
            double fudge = r.nextBoolean() ? r.nextDouble() * 1000 : r.nextDouble();
            double factor = r.nextBoolean() ? 12.75 : r.nextDouble() * 20;
            StringBuilder sb = new StringBuilder("fbm " + sd + " " + first + " " + Pts.h(fudge) + " " + Pts.h(factor) + "\n");
            Refl.describe(sb, BlendedNoise.createFbm(new XoroshiroRandomSource(sd), first, fudge, factor), 0);
            o.print(sb);
        }
        Pts g = new Pts(r.nextLong());
        for (int i = 0; i < inst; i++) {
            double xz = 0.25, y = 0.125, xf = 80, yf = 160, sm = 8;
            if (i >= 2) {
                xz = 0.001 + r.nextDouble() * (r.nextBoolean() ? 1 : 100);
                y = 0.001 + r.nextDouble() * (r.nextBoolean() ? 1 : 100);
                xf = 0.001 + r.nextDouble() * 200;
                yf = 0.001 + r.nextDouble() * 200;
                sm = 1 + r.nextDouble() * 7;
            }
            BlendedNoise b = new BlendedNoise(xz, y, xf, yf, sm);
            long sd = r.nextLong();
            boolean x = r.nextBoolean();
            RandomSource src = x ? new XoroshiroRandomSource(sd) : new LegacyRandomSource(sd);
            o.println("blended " + (x ? "xoro " : "legacy ") + sd + " " + Pts.h(xz) + " " + Pts.h(y) + " " + Pts.h(xf) + " " + Pts.h(yf) + " " + Pts.h(sm) + " range " + Pts.h(b.range().min()) + " " + Pts.h(b.range().max()));
            if (i < 2) {
                BlendedNoise.FbmSet set = b.createFbmSet(x ? new XoroshiroRandomSource(sd) : new LegacyRandomSource(sd));
                StringBuilder sb = new StringBuilder();
                Refl.describe(sb, set.minLimitNoise(), 0);
                Refl.describe(sb, set.maxLimitNoise(), 0);
                Refl.describe(sb, set.mainNoise(), 0);
                o.print(sb);
            }
            DensitySampler s = b.compileSampler(src);
            for (int k = 0; k < n; k++) {
                int bx = (int) Math.max(-30_000_000, Math.min(30_000_000, g.c())), by = r.nextInt(448) - 64, bz = (int) Math.max(-30_000_000, Math.min(30_000_000, g.c()));
                if (k % 3 == 0) { bx = r.nextInt(2000) - 1000; bz = r.nextInt(2000) - 1000; }
                o.println("v " + bx + " " + by + " " + bz + " = " + Pts.h(s.sampleValue(SamplerContext.EMPTY_UNCACHED, bx, by, bz)));
            }
        }
        o.flush();
    }
}
