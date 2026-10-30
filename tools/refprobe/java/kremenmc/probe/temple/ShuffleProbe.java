package kremenmc.probe.temple;

import java.util.ArrayList;
import java.util.List;
import net.minecraft.util.RandomSource;

// ShuffleProbe runs Util.shuffle on 0..n-1 with a logging random and prints
// the draws and the result; and RandomSource.create(seed)'s first longs.
// Args: n seed
public final class ShuffleProbe {
    public static void main(String[] args) {
        LogWR.OUT = System.out;
        int n = Integer.parseInt(args[0]);
        long seed = Long.parseLong(args[1]);
        List<Integer> l = new ArrayList<>();
        for (int i = 0; i < n; i++) l.add(i);
        LogRS r = new LogRS(RandomSource.create(seed));
        r.on = true;
        net.minecraft.util.Util.shuffle(l, r);
        System.out.println("RESULT " + l);
        var oal = new it.unimi.dsi.fastutil.objects.ObjectArrayList<Integer>();
        for (int i = 0; i < n; i++) oal.add(i);
        LogRS r2 = new LogRS(RandomSource.create(seed)); r2.on = true;
        System.out.println("SHUFFLEDCOPY " + net.minecraft.util.Util.shuffledCopy(oal, r2));
        LogRS r3 = new LogRS(RandomSource.create(seed)); r3.on = true;
        System.out.println("TOSHUFFLED " + net.minecraft.util.Util.toShuffledList(java.util.stream.IntStream.range(0, n).boxed(), r3));
        RandomSource c = RandomSource.create(seed);
        System.out.println("CREATE " + c.getClass().getName() + " " + c.nextLong() + " " + c.nextLong());
    }
}
