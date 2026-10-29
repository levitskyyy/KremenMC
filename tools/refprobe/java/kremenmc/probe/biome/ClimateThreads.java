package kremenmc.probe.biome;

import com.mojang.datafixers.util.Pair;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.SplittableRandom;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.biome.Climate;

// ClimateThreads <report.json> <threads> <count> <seed>
// Runs one boundary-target sequence per thread concurrently on a shared
// ParameterList, then replays each sequence alone on a fresh thread, and
// prints how many results differ (0 means the remembered result is purely
// per thread) plus how many results depend on the sequence at all.
public final class ClimateThreads {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        List<Pair<Climate.ParameterPoint, Integer>> entries = new ArrayList<>();
        for (var e : ClimateProbe.load(Path.of(args[0]))) entries.add(Pair.of(e.getFirst(), entries.size()));
        Climate.ParameterList<Integer> list = new Climate.ParameterList<>(entries);
        int threads = Integer.parseInt(args[1]), count = Integer.parseInt(args[2]);
        SplittableRandom rnd = new SplittableRandom(Long.parseLong(args[3]));
        Climate.TargetPoint[][] seqs = new Climate.TargetPoint[threads][count];
        for (Climate.TargetPoint[] s : seqs) {
            for (int i = 0; i < count; i++) {
                long[] v = new long[6];
                for (int d = 0; d < 6; d++) {
                    Climate.ParameterPoint p = entries.get(rnd.nextInt(entries.size())).getFirst();
                    Climate.Parameter q = List.of(p.temperature(), p.humidity(), p.continentalness(), p.erosion(),
                            p.depth(), p.weirdness()).get(d);
                    v[d] = (rnd.nextBoolean() ? q.min() : q.max()) + rnd.nextInt(-1, 2);
                }
                s[i] = new Climate.TargetPoint(v[0], v[1], v[2], v[3], v[4], v[5]);
            }
        }
        int[][] conc = new int[threads][count], alone = new int[threads][count], fresh = new int[threads][count];
        Thread[] ts = new Thread[threads];
        for (int t = 0; t < threads; t++) {
            int id = t;
            ts[t] = new Thread(() -> {
                for (int i = 0; i < count; i++) conc[id][i] = list.findValue(seqs[id][i]);
            });
        }
        for (Thread t : ts) t.start();
        for (Thread t : ts) t.join();
        for (int t = 0; t < threads; t++) {
            int id = t;
            Thread th = new Thread(() -> {
                for (int i = 0; i < count; i++) alone[id][i] = list.findValue(seqs[id][i]);
            });
            th.start();
            th.join();
            for (int i = 0; i < count; i++) {
                Climate.TargetPoint p = seqs[t][i];
                int[] r = new int[1];
                Thread f = new Thread(() -> r[0] = list.findValue(p));
                if (i < 20000) {
                    f.start();
                    f.join();
                    fresh[t][i] = r[0];
                } else {
                    fresh[t][i] = alone[t][i];
                }
            }
        }
        long diff = 0, dep = 0;
        for (int t = 0; t < threads; t++) {
            for (int i = 0; i < count; i++) {
                if (conc[t][i] != alone[t][i]) diff++;
                if (i < 20000 && alone[t][i] != fresh[t][i]) dep++;
            }
        }
        System.out.println("threads=" + threads + " count=" + count + " concurrent_vs_alone_diff=" + diff
                + " sequence_dependent(first 20000 per thread)=" + dep);
    }
}
