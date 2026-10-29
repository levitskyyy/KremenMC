package kremenmc.probe.feat_ore;

import java.util.ArrayDeque;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;

// OreScriptRandom returns scripted values for nextFloat/nextInt(n)/nextDouble
// (in call order, one queue) and fails loudly on anything else or when the
// script runs out.
class OreScriptRandom implements RandomSource {
    final ArrayDeque<Double> q = new ArrayDeque<>();
    int calls;

    private double pop(String what) {
        calls++;
        if (q.isEmpty()) throw new IllegalStateException("script exhausted at " + what);
        return q.pop();
    }

    public RandomSource fork() { throw new UnsupportedOperationException("fork"); }
    public PositionalRandomFactory forkPositional() { throw new UnsupportedOperationException("forkPositional"); }
    public void setSeed(long s) { throw new UnsupportedOperationException("setSeed"); }
    public int nextInt() { throw new UnsupportedOperationException("nextInt()"); }
    public int nextInt(int b) { return (int) pop("nextInt(" + b + ")"); }
    public long nextLong() { return 0; }
    public boolean nextBoolean() { throw new UnsupportedOperationException("nextBoolean"); }
    public float nextFloat() { return (float) pop("nextFloat"); }
    public double nextDouble() { return pop("nextDouble"); }
    public double nextGaussian() { throw new UnsupportedOperationException("nextGaussian"); }
}
