package kremenmc.probe.biome;

import com.mojang.datafixers.util.Pair;
import java.lang.reflect.Field;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.biome.Climate;

// ClimateGolden <run>... where run = list|k|mode|count|seed|sample
//
//	list: report:<path> | syn:<style>:<n>:<seed>  (style grid|sparse|dup|wide)
//	k:    children per node (0 = default constructor)
//	mode: random | boundary | tie
//
// For each run prints the entry count and hash, then
// findValue results as entry indices for a fresh thread state ("fresh",
// per-thread last result cleared before every query) and for one thread
// querying in order ("seq"): S lines for the first `sample` targets, B lines
// with FNV hashes per block of 65536 targets, and a T line for the run.
// Target and synthetic list generation uses Rng (SplitMix64), mirrored in Go.
public final class ClimateGolden {
    static final int BLOCK = 1 << 16;

    static final class Rng {
        long s;

        Rng(long seed) { s = seed; }

        long next() {
            long z = (s += 0x9E3779B97F4A7C15L);
            z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9L;
            z = (z ^ (z >>> 27)) * 0x94D049BB133111EBL;
            return z ^ (z >>> 31);
        }

        long below(long n) { return (next() >>> 1) % n; }

        long range(long lo, long hi) { return lo + below(hi - lo + 1); }
    }

    static long fnv(long h, long v) { return (h ^ v) * 0x100000001B3L; }

    static List<long[]> synthetic(String style, int n, long seed) {
        Rng r = new Rng(seed);
        List<long[]> out = new ArrayList<>();
        List<long[]> pool = new ArrayList<>();
        for (int i = 0; i < n; i++) {
            long[] v = new long[13];
            if (style.equals("dup") && !pool.isEmpty() && r.below(3) != 0) {
                out.add(pool.get((int) r.below(pool.size())).clone());
                continue;
            }
            for (int d = 0; d < 6; d++) {
                long a, w;
                switch (style) {
                    case "wide" -> { a = r.range(-100, 100); w = r.range(0, 50); }
                    case "sparse" -> {
                        if (d == 1 || d == 3 || d == 4) { a = 0; w = 0; }
                        else { a = r.range(-3, 3); w = r.range(0, 2); }
                    }
                    default -> { a = r.range(-3, 3); w = r.range(0, 3); }
                }
                v[2 * d] = a;
                v[2 * d + 1] = a + w;
            }
            v[12] = style.equals("wide") ? 0 : Math.max(0, r.range(-2, 2));
            out.add(v);
            pool.add(v);
        }
        return out;
    }

    static Climate.Parameter[] axes(Climate.ParameterPoint p) {
        return new Climate.Parameter[] {p.temperature(), p.humidity(), p.continentalness(), p.erosion(), p.depth(),
            p.weirdness()};
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        StringBuilder sb = new StringBuilder();
        for (String run : args) {
            String[] f = run.split("\\|");
            List<Pair<Climate.ParameterPoint, Integer>> entries = new ArrayList<>();
            boolean syn = f[0].startsWith("syn:");
            if (syn) {
                String[] s = f[0].split(":");
                for (long[] v : synthetic(s[1], Integer.parseInt(s[2]), Long.parseLong(s[3]))) {
                    entries.add(Pair.of(new Climate.ParameterPoint(new Climate.Parameter(v[0], v[1]),
                            new Climate.Parameter(v[2], v[3]), new Climate.Parameter(v[4], v[5]),
                            new Climate.Parameter(v[6], v[7]), new Climate.Parameter(v[8], v[9]),
                            new Climate.Parameter(v[10], v[11]), v[12]), entries.size()));
                }
            } else {
                for (var e : ClimateProbe.load(Path.of(f[0].substring("report:".length())))) {
                    entries.add(Pair.of(e.getFirst(), entries.size()));
                }
            }
            int k = Integer.parseInt(f[1]);
            String mode = f[2];
            int count = Integer.parseInt(f[3]);
            Rng rng = new Rng(Long.parseLong(f[4]));
            int sample = Integer.parseInt(f[5]);

            long eh = 0xCBF29CE484222325L;
            long[][] lo = new long[entries.size()][6], hi = new long[entries.size()][6];
            long[] min = new long[6], max = new long[6];
            java.util.Arrays.fill(min, Long.MAX_VALUE);
            java.util.Arrays.fill(max, Long.MIN_VALUE);
            for (int i = 0; i < entries.size(); i++) {
                Climate.ParameterPoint p = entries.get(i).getFirst();
                Climate.Parameter[] a = axes(p);
                for (int d = 0; d < 6; d++) {
                    lo[i][d] = a[d].min();
                    hi[i][d] = a[d].max();
                    min[d] = Math.min(min[d], lo[i][d]);
                    max[d] = Math.max(max[d], hi[i][d]);
                    eh = fnv(fnv(eh, lo[i][d]), hi[i][d]);
                }
                eh = fnv(eh, p.offset());
            }
            sb.append("R ").append(run).append(" entries=").append(entries.size()).append(" hash=")
                    .append(Long.toHexString(eh)).append('\n');

            Climate.ParameterList<Integer> list = new Climate.ParameterList<>(entries);
            if (k > 0) list = list.rebuildWithChildrenPerNode(k);
            Field tf = Climate.ParameterList.class.getDeclaredField("index");
            tf.setAccessible(true);
            Object tree = tf.get(list);
            Field lf = tree.getClass().getDeclaredField("lastResult");
            lf.setAccessible(true);
            ThreadLocal<?> last = (ThreadLocal<?>) lf.get(tree);

            int n = entries.size();
            long[] pad = new long[6];
            for (int d = 0; d < 6; d++) pad[d] = syn ? 2 : 2000;
            long bf = 0xCBF29CE484222325L, bs = bf, tf2 = bf, ts = bf;
            int diff = 0;
            long[] v = new long[6];
            for (int i = 0; i < count; i++) {
                for (int d = 0; d < 6; d++) {
                    switch (mode) {
                        case "random" -> v[d] = rng.range(min[d] - pad[d], max[d] + pad[d]);
                        case "boundary" -> {
                            int e = (int) rng.below(n);
                            v[d] = (rng.below(2) == 0 ? lo[e][d] : hi[e][d]) + rng.range(-1, 1);
                        }
                        default -> {
                            int e = (int) rng.below(n), e2 = (int) rng.below(n);
                            long a = rng.below(2) == 0 ? lo[e][d] : hi[e][d];
                            long b = rng.below(2) == 0 ? lo[e2][d] : hi[e2][d];
                            v[d] = switch ((int) rng.below(3)) {
                                case 0 -> a;
                                case 1 -> (a + b) / 2;
                                default -> a + rng.range(-1, 1);
                            };
                        }
                    }
                }
                Climate.TargetPoint t = new Climate.TargetPoint(v[0], v[1], v[2], v[3], v[4], v[5]);
                int seq = list.findValue(t);
                Object keep = last.get();
                last.remove();
                int fresh = list.findValue(t);
                restore(last, keep);
                if (fresh != seq) diff++;
                if (i < sample) {
                    sb.append("S");
                    for (long x : v) sb.append(' ').append(x);
                    sb.append(' ').append(fresh).append(' ').append(seq).append('\n');
                }
                bf = fnv(bf, fresh);
                bs = fnv(bs, seq);
                tf2 = fnv(tf2, fresh);
                ts = fnv(ts, seq);
                if ((i + 1) % BLOCK == 0 || i + 1 == count) {
                    sb.append("B ").append(i / BLOCK).append(' ').append(Long.toHexString(bf)).append(' ')
                            .append(Long.toHexString(bs)).append('\n');
                    bf = bs = 0xCBF29CE484222325L;
                }
                if (sb.length() > 1 << 16) {
                    System.out.print(sb);
                    sb.setLength(0);
                }
            }
            sb.append("T ").append(Long.toHexString(tf2)).append(' ').append(Long.toHexString(ts)).append(" seqdiff=")
                    .append(diff).append('\n');
        }
        System.out.print(sb);
        System.out.flush();
    }

    @SuppressWarnings("unchecked")
    static <L> void restore(ThreadLocal<L> tl, Object v) {
        if (v == null) tl.remove();
        else tl.set((L) v);
    }
}
