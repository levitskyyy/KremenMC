package kremenmc.probe.feat_cave;

import java.util.stream.Collectors;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;

// LogRandom delegates to a random source and prints every call with its
// result and the reference call path that made it.
class LogRandom implements RandomSource {
    static java.io.PrintStream OUT;
    final RandomSource d;
    boolean on = true;
    int n;

    LogRandom(RandomSource d) { this.d = d; }

    private static final StackWalker W = StackWalker.getInstance();

    static String path() {
        return W.walk(s -> s.filter(f -> f.getClassName().startsWith("net.minecraft"))
            .map(f -> f.getClassName().substring(f.getClassName().lastIndexOf('.') + 1) + "." + f.getMethodName())
            .limit(12).collect(Collectors.joining("<")));
    }

    private <T> T log(String what, T v) {
        if (on) OUT.println("R " + (n++) + " " + what + " = " + v + " @" + path());
        return v;
    }

    public RandomSource fork() { return log("fork()", d.fork()); }
    public PositionalRandomFactory forkPositional() { return log("forkPositional()", d.forkPositional()); }
    public void setSeed(long s) { log("setSeed(" + s + ")", ""); d.setSeed(s); }
    public int nextInt() { return log("nextInt()", d.nextInt()); }
    public int nextInt(int b) { return log("nextInt(" + b + ")", d.nextInt(b)); }
    public int nextIntBetweenInclusive(int a, int b) { return log("nextIntBetweenInclusive(" + a + "," + b + ")", d.nextIntBetweenInclusive(a, b)); }
    public int nextInt(int a, int b) { return log("nextInt(" + a + "," + b + ")", d.nextInt(a, b)); }
    public long nextLong() { return log("nextLong()", d.nextLong()); }
    public boolean nextBoolean() { return log("nextBoolean()", d.nextBoolean()); }
    public float nextFloat() { return log("nextFloat()", d.nextFloat()); }
    public double nextDouble() { return log("nextDouble()", d.nextDouble()); }
    public double nextGaussian() { return log("nextGaussian()", d.nextGaussian()); }
    public double triangle(double a, double b) { return log("triangle(" + a + "," + b + ")", d.triangle(a, b)); }
    public float triangle(float a, float b) { return log("triangleF(" + a + "," + b + ")", d.triangle(a, b)); }
    public void consumeCount(int c) { log("consumeCount(" + c + ")", ""); d.consumeCount(c); }
}
