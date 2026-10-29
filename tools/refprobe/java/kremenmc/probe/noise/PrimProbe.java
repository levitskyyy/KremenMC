package kremenmc.probe.noise;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.lang.reflect.Method;
import java.util.Random;
import net.minecraft.SharedConstants;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.synth.PerlinNoise;
import net.minecraft.world.level.levelgen.synth.SimplexNoise;
import net.minecraft.world.level.levelgen.synth.SmearedPerlinNoise;

// Single-octave primitives: Perlin (state, get, derivative, sampleAndLerp),
// simplex (both offset modes) and smeared Perlin.
// args: <seed> <instancesPerKind> <samplesPerInstance>
public final class PrimProbe {
    static final long[] SEEDS = {0L, -1L, 1L, Long.MIN_VALUE, Long.MAX_VALUE, 42L};

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Random r = new Random(Long.parseLong(args[0]));
        int inst = Integer.parseInt(args[1]), n = Integer.parseInt(args[2]);
        PrintStream o = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false);
        Method sl = PerlinNoise.class.getDeclaredMethod("sampleAndLerp", int.class, int.class, int.class, float.class, float.class, float.class, float.class);
        sl.setAccessible(true);
        Pts g = new Pts(r.nextLong());
        for (int i = 0; i < inst; i++) {
            boolean x = r.nextBoolean();
            long sd = i < SEEDS.length ? SEEDS[i] : r.nextLong();
            RandomSource src = x ? new XoroshiroRandomSource(sd) : new LegacyRandomSource(sd);
            String alg = (x ? "xoro " : "legacy ") + sd;
            switch (i % 4) {
                case 0, 1 -> {
                    PerlinNoise p = new PerlinNoise(src);
                    o.println("perlin " + alg);
                    o.println("state " + Refl.gradient(p) + " next " + src.nextLong());
                    for (int k = 0; k < n; k++) {
                        double a = g.c(), b = g.y(), c = g.c();
                        if (k % 5 == 0) { a = (g.r.nextDouble() - 0.5) * 40; b = (g.r.nextDouble() - 0.5) * 40; c = (g.r.nextDouble() - 0.5) * 40; }
                        float[] d = {0.25f, -1f, 3f};
                        float v = p.noiseWithDerivative(a, b, c, d);
                        o.println("g " + Pts.h(a) + " " + Pts.h(b) + " " + Pts.h(c) + " = " + Pts.h(p.get(a, b, c)) + " " + Pts.h(p.get(a, c)) + " " + Pts.h(v) + " " + Pts.h(d[0]) + " " + Pts.h(d[1]) + " " + Pts.h(d[2]));
                        if (k % 4 == 0) {
                            int gx = g.r.nextInt(), gy = g.r.nextInt(), gz = g.r.nextInt();
                            float fx = g.r.nextFloat(), fy = g.r.nextFloat(), fz = g.r.nextFloat(), fw = g.r.nextBoolean() ? fy : g.r.nextFloat();
                            o.println("sl " + gx + " " + gy + " " + gz + " " + Pts.h(fx) + " " + Pts.h(fy) + " " + Pts.h(fz) + " " + Pts.h(fw) + " = " + Pts.h((float) sl.invoke(p, gx, gy, gz, fx, fy, fz, fw)));
                        }
                    }
                }
                case 2 -> {
                    boolean flag = r.nextBoolean(), dflt = r.nextInt(3) == 0;
                    SimplexNoise s = dflt ? new SimplexNoise(src) : new SimplexNoise(src, flag);
                    if (dflt) flag = false;
                    o.println("simplex " + alg + " " + flag + " " + Refl.gradient(s));
                    for (int k = 0; k < n; k++) {
                        double a = g.c(), b = g.y(), c = g.c();
                        if (k % 3 == 0) { a = (g.r.nextDouble() - 0.5) * 40; b = (g.r.nextDouble() - 0.5) * 40; c = (g.r.nextDouble() - 0.5) * 40; }
                        o.println("s " + Pts.h(a) + " " + Pts.h(b) + " " + Pts.h(c) + " = " + Pts.h(s.get(a, b, c)) + " " + Pts.h(s.get(a, c)));
                    }
                }
                default -> {
                    double fudge = switch (r.nextInt(6)) {
                        case 0 -> 0.0;
                        case 1 -> r.nextDouble();
                        case 2 -> r.nextDouble() * 1e-9;
                        case 3 -> 1 + r.nextDouble() * 10;
                        default -> 684.412 * (0.001 + r.nextDouble() * 0.01);
                    };
                    SmearedPerlinNoise s = new SmearedPerlinNoise(src, fudge);
                    o.println("smeared " + alg + " " + Pts.h(fudge) + " " + Pts.h(s.range().max()) + " " + Refl.gradient(s));
                    for (int k = 0; k < n; k++) {
                        double a = g.c(), b = g.y(), c = g.c();
                        if (k % 3 == 0) { a = (g.r.nextDouble() - 0.5) * 40; b = (g.r.nextDouble() - 0.5) * 4; c = (g.r.nextDouble() - 0.5) * 40; }
                        if (k % 7 == 1) b = g.r.nextDouble();
                        o.println("s " + Pts.h(a) + " " + Pts.h(b) + " " + Pts.h(c) + " = " + Pts.h(s.get(a, b, c)) + " " + Pts.h(s.get(a, c)));
                    }
                }
            }
        }
        o.flush();
    }
}
