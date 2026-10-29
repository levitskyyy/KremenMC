package kremenmc.probe.carver;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.util.TreeSet;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.chunk.CarverOutput;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.carver.WorldCarver;

// Trace runs a carver (registry id or inline JSON) for source/target chunk
// pairs with a logging random and reports, per case, the random calls and
// carve calls in order (mode "trace", meant to run under Jdi) or the CRC32
// of the sorted carved set (mode "set").
// Args: <trace|set> <carver|@json file> <dim> <seed:cx:cz:sx:sz | @case file>...
public final class Trace {
    static WorldCarver parse(String s) throws java.io.IOException {
        if (s.startsWith("@")) s = java.nio.file.Files.readString(java.nio.file.Path.of(s.substring(1)));
        if (!s.startsWith("{")) return Env.carver(s);
        return WorldCarver.DIRECT_CODEC.parse(JsonOps.INSTANCE, JsonParser.parseString(s)).getOrThrow();
    }

    public static void main(String[] a) throws Exception {
        Env.init();
        boolean trace = a[0].equals("trace");
        var carver = parse(a[1]);
        var ctx = Env.ctx(a[2]);
        if (trace) Env.out.println("ctx minGenY " + ctx.getMinGenY() + " depth " + ctx.getGenDepth() + " sea " + ctx.seaLevel() + " range " + carver.getRange());
        int minY = ctx.getMinGenY(), maxY = minY + ctx.getGenDepth() - 1;
        java.util.List<String> cases = new java.util.ArrayList<>();
        for (int i = 3; i < a.length; i++) {
            if (a[i].startsWith("@")) cases.addAll(java.nio.file.Files.readAllLines(java.nio.file.Path.of(a[i].substring(1))));
            else cases.add(a[i]);
        }
        for (String cs : cases) {
            String[] f = cs.split(":");
            long seed = Long.parseLong(f[0]);
            var chunk = new ChunkPos(Integer.parseInt(f[1]), Integer.parseInt(f[2]));
            var src = new ChunkPos(Integer.parseInt(f[3]), Integer.parseInt(f[4]));
            var r = new LogRandom(new LegacyRandomSource(seed));
            r.on = trace;
            TreeSet<Long> set = new TreeSet<>();
            CarverOutput o = new CarverOutput() {
                public void carve(int x, int y, int z) {
                    if (trace) Hook.ev("C " + x + " " + y + " " + z);
                    set.add(((long) y << 16) | (z << 4) | x);
                }
                public int minY() { return minY; }
                public int maxY() { return maxY; }
            };
            if (trace) {
                Hook.ev("case " + cs);
                Hook.ev("BEGIN");
            }
            boolean start = carver.isStartChunk(r);
            boolean res = carver.carve(ctx, r, chunk, src, o);
            if (trace) Hook.ev("END");
            var crc = new java.util.zip.CRC32();
            for (long p : set) crc.update((p + "\n").getBytes());
            String line = start + " " + res + " " + set.size() + " " + Long.toHexString(crc.getValue());
            if (trace) Hook.ev("= " + cs + " " + line);
            else Env.out.println(line);
        }
        Env.out.flush();
    }
}
