package kremenmc.probe.aquifer;

import java.util.List;
import java.util.Random;

// Single-point values of the six aquifer config functions. Each value is
// sampled uncached and through the aquifer's own bound sampler (of the
// chunk holding the point); a disagreement is reported as an error line.
// args: <settings> <seed> <points> <pointSeed>
public final class Values {
    static final List<String> FNS = List.of("barrier", "exclusion", "fluid_level_floodedness", "fluid_level_spread", "lava", "surface_level");

    public static void main(String[] args) throws Exception {
        Env.init();
        String st = args[0];
        long seed = Long.parseLong(args[1]);
        int n = Integer.parseInt(args[2]);
        Random r = new Random(Long.parseLong(args[3]));
        int minY = Env.settings(st).noiseSettings().minY(), h = Env.settings(st).noiseSettings().height();
        int[][] pts = new int[n][];
        for (int i = 0; i < n; i++) {
            int x, y, z;
            switch (i % 6) {
                case 0 -> { x = r.nextInt(2001) - 1000; z = r.nextInt(2001) - 1000; }
                case 1 -> { x = r.nextInt(161) - 80; z = r.nextInt(161) - 80; }
                case 2 -> { x = (r.nextInt(81) - 40) * 16 + r.nextInt(3) - 1; z = (r.nextInt(81) - 40) * 16 + r.nextInt(3) - 1; }
                case 3 -> { x = r.nextInt(60_000_001) - 30_000_000; z = r.nextInt(60_000_001) - 30_000_000; }
                case 4 -> { x = r.nextInt(2_000_001) - 1_000_000; z = r.nextInt(2_000_001) - 1_000_000; }
                default -> { x = r.nextInt(20001) - 10000; z = r.nextInt(20001) - 10000; }
            }
            y = switch (r.nextInt(4)) { case 0 -> r.nextInt(41) - 20; case 1 -> minY + r.nextInt(8); default -> minY + r.nextInt(h); };
            pts[i] = new int[]{x, y, z};
        }
        StringBuilder sb = new StringBuilder("points");
        for (int[] p : pts) sb.append(' ').append(p[0]).append(',').append(p[1]).append(',').append(p[2]);
        Env.out.println(sb);
        Env.Chunk[] chunks = new Env.Chunk[n];
        for (int i = 0; i < n; i++) chunks[i] = Env.chunk(st, seed, pts[i][0] >> 4, pts[i][2] >> 4);
        for (String fn : FNS) {
            sb = new StringBuilder("f ").append(fn);
            for (int i = 0; i < n; i++) {
                int[] p = pts[i];
                float u = chunks[i].uncached(fn, p[0], p[1], p[2]);
                float b = chunks[i].bound(chunks[i].aquifer(), fn, p[0], p[1], p[2]);
                if (Float.floatToRawIntBits(u) != Float.floatToRawIntBits(b)) Env.out.println("error bound " + fn + " " + p[0] + "," + p[1] + "," + p[2] + " " + u + " " + b);
                sb.append(' ').append(Integer.toHexString(Float.floatToRawIntBits(u)));
            }
            Env.out.println(sb);
        }
        Env.out.flush();
    }
}
