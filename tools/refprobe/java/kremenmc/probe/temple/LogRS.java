package kremenmc.probe.temple;

import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;

// LogRS delegates to a RandomSource and logs every top-level draw ("Q"
// lines) with its value and call path (LogWR.path), for level.getRandom().
final class LogRS implements RandomSource {
    final RandomSource d;
    boolean on;
    int n, depth;

    LogRS(RandomSource d) { this.d = d; }

    private <T> T log(String what, T v) {
        if (on && depth == 1) LogWR.OUT.println("Q " + (n++) + " " + what + " = " + v + " @" + LogWR.path());
        return v;
    }

    public RandomSource fork() { return d.fork(); }
    public PositionalRandomFactory forkPositional() { return d.forkPositional(); }
    public void setSeed(long s) { d.setSeed(s); }
    public int nextInt() { depth++; try { return log("nextInt()", d.nextInt()); } finally { depth--; } }
    public int nextInt(int b) { depth++; try { return log("nextInt(" + b + ")", d.nextInt(b)); } finally { depth--; } }
    public int nextIntBetweenInclusive(int a, int b) { depth++; try { return log("between(" + a + "," + b + ")", d.nextIntBetweenInclusive(a, b)); } finally { depth--; } }
    public int nextInt(int a, int b) { depth++; try { return log("range(" + a + "," + b + ")", d.nextInt(a, b)); } finally { depth--; } }
    public long nextLong() { depth++; try { return log("nextLong()", d.nextLong()); } finally { depth--; } }
    public boolean nextBoolean() { depth++; try { return log("nextBoolean()", d.nextBoolean()); } finally { depth--; } }
    public float nextFloat() { depth++; try { return log("nextFloat()", d.nextFloat()); } finally { depth--; } }
    public double nextDouble() { depth++; try { return log("nextDouble()", d.nextDouble()); } finally { depth--; } }
    public double nextGaussian() { depth++; try { return log("nextGaussian()", d.nextGaussian()); } finally { depth--; } }
    public double triangle(double a, double b) { depth++; try { return log("triangle(" + a + "," + b + ")", d.triangle(a, b)); } finally { depth--; } }
    public float triangle(float a, float b) { depth++; try { return log("trianglef(" + a + "," + b + ")", d.triangle(a, b)); } finally { depth--; } }
    public void consumeCount(int c) { depth++; try { log("consume(" + c + ")", c); d.consumeCount(c); } finally { depth--; } }
}
