package kremenmc.probe.rportal;

import java.util.stream.Collectors;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.WorldgenRandom;

// LogWR is a WorldgenRandom over a legacy source that logs every
// high-level draw with its value and the reference call path.
class LogWR extends WorldgenRandom {
    static java.io.PrintStream OUT;
    static int LIMIT = System.getenv("PATHLIM") != null ? Integer.parseInt(System.getenv("PATHLIM")) : 5;

    static int nest() {
        return W.walk(st -> st.filter(f -> f.getMethodName().equals("addChildren")).count()).intValue();
    }
    boolean on;
    int n;
    int depth;

    LogWR() { super(new LegacyRandomSource(0)); }
    LogWR(net.minecraft.util.RandomSource src) { super(src); }

    private static final StackWalker W = StackWalker.getInstance();

    static String path() {
        return W.walk(s -> s.filter(f -> f.getClassName().startsWith("net.minecraft"))
            .map(f -> { String c = f.getClassName(); c = c.substring(c.lastIndexOf('.') + 1); c = c.substring(c.indexOf('$') + 1); return c + "." + f.getMethodName(); })
            .filter(x -> !x.startsWith("WorldgenRandom") && !x.startsWith("BitRandomSource") && !x.startsWith("LegacyRandomSource"))
            .limit(LIMIT).collect(Collectors.joining("<")));
    }

    private <T> T log(String what, T v) {
        if (on && depth == 1) OUT.println("R" + nest() + " " + (n++) + " " + what + " = " + v + " @" + path());
        return v;
    }

    public int nextInt(int b) { depth++; try { return log("nextInt(" + b + ")", super.nextInt(b)); } finally { depth--; } }
    public int nextInt() { depth++; try { return log("nextInt()", super.nextInt()); } finally { depth--; } }
    public long nextLong() { depth++; try { return log("nextLong()", super.nextLong()); } finally { depth--; } }
    public boolean nextBoolean() { depth++; try { return log("nextBoolean()", super.nextBoolean()); } finally { depth--; } }
    public float nextFloat() { depth++; try { return log("nextFloat()", super.nextFloat()); } finally { depth--; } }
    public double nextDouble() { depth++; try { return log("nextDouble()", super.nextDouble()); } finally { depth--; } }
    public int nextIntBetweenInclusive(int a, int b) { depth++; try { return log("between(" + a + "," + b + ")", super.nextIntBetweenInclusive(a, b)); } finally { depth--; } }
}
