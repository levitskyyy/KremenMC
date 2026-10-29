package kremenmc.probe.explosion;

import java.util.List;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;

// Rec wraps a RandomSource and logs every call at the interface level:
// "r <tag> <method> <args...> = <result>". Floats/doubles are logged as raw
// IEEE bits in hex so the Go side can compare exactly.
final class Rec implements RandomSource {
    final RandomSource d;
    final String tag;
    final List<String> log;
    boolean on = true;
    Runnable before;

    Rec(RandomSource d, String tag, List<String> log) {
        this.d = d;
        this.tag = tag;
        this.log = log;
    }

    private void put(String s) {
        if (before != null) before.run();
        if (on) log.add("r " + tag + " " + s);
    }

    static String f(float v) {
        return "f" + Integer.toHexString(Float.floatToRawIntBits(v));
    }

    static String d(double v) {
        return "d" + Long.toHexString(Double.doubleToRawLongBits(v));
    }

    public RandomSource fork() {
        put("fork");
        return d.fork();
    }

    public PositionalRandomFactory forkPositional() {
        put("forkPositional");
        return d.forkPositional();
    }

    public void setSeed(long s) {
        put("setSeed " + s);
        d.setSeed(s);
    }

    public int nextInt() {
        int v = d.nextInt();
        put("nextInt = " + v);
        return v;
    }

    public int nextInt(int b) {
        int v = d.nextInt(b);
        put("nextIntn " + b + " = " + v);
        return v;
    }

    public int nextIntBetweenInclusive(int a, int b) {
        int v = d.nextIntBetweenInclusive(a, b);
        put("nextIntBetweenInclusive " + a + " " + b + " = " + v);
        return v;
    }

    public int nextInt(int a, int b) {
        int v = d.nextInt(a, b);
        put("nextIntRange " + a + " " + b + " = " + v);
        return v;
    }

    public long nextLong() {
        long v = d.nextLong();
        put("nextLong = " + v);
        return v;
    }

    public boolean nextBoolean() {
        boolean v = d.nextBoolean();
        put("nextBool = " + v);
        return v;
    }

    public float nextFloat() {
        float v = d.nextFloat();
        put("nextFloat = " + f(v));
        return v;
    }

    public double nextDouble() {
        double v = d.nextDouble();
        put("nextDouble = " + d(v));
        return v;
    }

    public double nextGaussian() {
        double v = d.nextGaussian();
        put("nextGaussian = " + d(v));
        return v;
    }

    public double triangle(double m, double dev) {
        double v = d.triangle(m, dev);
        put("triangle " + d(m) + " " + d(dev) + " = " + d(v));
        return v;
    }

    public float triangle(float m, float dev) {
        float v = d.triangle(m, dev);
        put("triangleF " + f(m) + " " + f(dev) + " = " + f(v));
        return v;
    }

    public void consumeCount(int n) {
        put("consume " + n);
        d.consumeCount(n);
    }
}
