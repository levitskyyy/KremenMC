package kremenmc.probe.noise;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.util.Random;
import net.minecraft.SharedConstants;
import net.minecraft.util.Mth;

// Prints Mth helper results for random and edge inputs. args: <seed> <n>
public final class MthProbe {
    static final double[] EDGE = {0.0, -0.0, 1.0, -1.0, 0.5, -0.5, 1e-300, -1e-300, Double.MIN_VALUE, 1e20, -1e20, 3e9, -3e9,
            2147483647.5, -2147483648.5, 9.3e18, -9.3e18, 1e30, -1e30, Double.MAX_VALUE, -Double.MAX_VALUE,
            Double.POSITIVE_INFINITY, Double.NEGATIVE_INFINITY, Double.NaN, 0.9999999999999999, -0.9999999999999999, 33554432.0, -33554432.5};

    static Random r;

    static double d() {
        return switch (r.nextInt(5)) {
            case 0 -> EDGE[r.nextInt(EDGE.length)];
            case 1 -> r.nextDouble();
            case 2 -> (r.nextDouble() - 0.5) * 4;
            case 3 -> (r.nextDouble() - 0.5) * Math.pow(2, r.nextInt(70));
            default -> Double.longBitsToDouble(r.nextLong());
        };
    }

    static float f() {
        return r.nextInt(6) == 0 ? Float.intBitsToFloat(r.nextInt()) : (float) d();
    }

    static String h(double v) {
        return Long.toUnsignedString(Double.doubleToRawLongBits(v), 16);
    }

    static String h(float v) {
        return Integer.toUnsignedString(Float.floatToRawIntBits(v), 16);
    }

    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        r = new Random(Long.parseLong(args[0]));
        int n = Integer.parseInt(args[1]);
        PrintStream o = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false);
        for (double e : EDGE) {
            o.println("floor " + h(e) + " = " + Mth.floor(e));
            o.println("lfloor " + h(e) + " = " + Mth.lfloor(e));
            o.println("ceil " + h(e) + " = " + Mth.ceil(e));
            o.println("floorf " + h((float) e) + " = " + Mth.floor((float) e));
            o.println("log " + h(Math.abs(e)) + " = " + h(StrictMath.log(Math.abs(e))));
        }
        for (int i = 0; i < n; i++) {
            double a = d(), b = d(), c = d(), t = r.nextBoolean() ? r.nextDouble() * 1.4 - 0.2 : d(), u = r.nextDouble(), w = r.nextDouble();
            float fa = f(), fb = f(), fc = f(), ft = r.nextBoolean() ? r.nextFloat() * 1.4f - 0.2f : f(), fu = r.nextFloat(), fw = r.nextFloat();
            switch (i % 18) {
                case 0 -> o.println("floor " + h(a) + " = " + Mth.floor(a));
                case 1 -> o.println("lfloor " + h(a) + " = " + Mth.lfloor(a));
                case 2 -> o.println("ceil " + h(a) + " = " + Mth.ceil(a));
                case 3 -> o.println("floorf " + h(fa) + " = " + Mth.floor(fa));
                case 4 -> o.println("lerp " + h(t) + " " + h(a) + " " + h(b) + " = " + h(Mth.lerp(t, a, b)));
                case 5 -> o.println("lerpf " + h(ft) + " " + h(fa) + " " + h(fb) + " = " + h(Mth.lerp(ft, fa, fb)));
                case 6 -> o.println("lerp3 " + h(t) + " " + h(u) + " " + h(w) + " " + h(a) + " " + h(b) + " " + h(c) + " " + h(a * 0.5) + " " + h(b - 1) + " " + h(c * 3) + " " + h(-a) + " " + h(u)
                        + " = " + h(Mth.lerp3(t, u, w, a, b, c, a * 0.5, b - 1, c * 3, -a, u)));
                case 7 -> o.println("lerp3f " + h(ft) + " " + h(fu) + " " + h(fw) + " " + h(fa) + " " + h(fb) + " " + h(fc) + " " + h(fa * 0.5f) + " " + h(fb - 1) + " " + h(fc * 3) + " " + h(-fa) + " " + h(fu)
                        + " = " + h(Mth.lerp3(ft, fu, fw, fa, fb, fc, fa * 0.5f, fb - 1, fc * 3, -fa, fu)));
                case 8 -> o.println("smooth " + h(ft) + " = " + h(Mth.smoothstep(ft)) + " " + h(Mth.smoothstepDerivative(ft)));
                case 9 -> o.println("clampedlerp " + h(t) + " " + h(a) + " " + h(b) + " = " + h(Mth.clampedLerp(t, a, b)));
                case 10 -> o.println("clampedlerpf " + h(ft) + " " + h(fa) + " " + h(fb) + " = " + h(Mth.clampedLerp(ft, fa, fb)));
                case 11 -> o.println("invlerp " + h(a) + " " + h(b) + " " + h(c) + " = " + h(Mth.inverseLerp(a, b, c)));
                case 12 -> o.println("map " + h(t) + " " + h(u) + " " + h(w) + " " + h(a) + " " + h(b) + " = " + h(Mth.map(t, u, w, a, b)) + " " + h(Mth.clampedMap(t, u, w, a, b)));
                case 13 -> o.println("mapf " + h(ft) + " " + h(fu) + " " + h(fw) + " " + h(fa) + " " + h(fb) + " = " + h(Mth.map(ft, fu, fw, fa, fb)) + " " + h(Mth.clampedMap(ft, fu, fw, fa, fb)));
                case 14 -> o.println("clamp " + h(a) + " " + h(Math.min(b, c)) + " " + h(Math.max(b, c)) + " = " + h(Mth.clamp(a, Math.min(b, c), Math.max(b, c))));
                case 15 -> o.println("clampf " + h(fa) + " " + h(Math.min(fb, fc)) + " " + h(Math.max(fb, fc)) + " = " + h(Mth.clamp(fa, Math.min(fb, fc), Math.max(fb, fc))));
                case 16 -> o.println("frac " + h(a) + " = " + h(Mth.frac(a)));
                default -> {
                    double x = r.nextBoolean() ? r.nextDouble() : Math.abs(d());
                    o.println("log " + h(x) + " = " + h(StrictMath.log(x)));
                }
            }
        }
        o.flush();
    }
}
