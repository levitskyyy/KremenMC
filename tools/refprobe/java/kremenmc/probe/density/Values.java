package kremenmc.probe.density;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import net.minecraft.core.registries.Registries;
import net.minecraft.world.level.biome.Climate;
import net.minecraft.world.level.levelgen.NoiseRouter;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.densityfunction.DensityFunction;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;

// Single-point values of router fields (and optionally every registered
// density function) plus climate target points.
// args: <settings> <seed> <points> <registry:0|1> <pointSeed>
public final class Values {
    static String f(float v) {
        return "f" + Integer.toHexString(Float.floatToRawIntBits(v));
    }

    public static void main(String[] args) {
        World.init();
        String st = args[0];
        long seed = Long.parseLong(args[1]);
        int n = Integer.parseInt(args[2]);
        boolean reg = args[3].equals("1");
        Random r = new Random(Long.parseLong(args[4]));
        PrintStream o = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false);
        RandomState rs = World.state(st, seed);
        NoiseRouter rt = World.settings(st).noiseRouter();
        int minY = World.settings(st).noiseSettings().minY(), h = World.settings(st).noiseSettings().height();
        int[][] pts = new int[n][];
        for (int i = 0; i < n; i++) {
            int x, y, z;
            switch (i % 6) {
                case 0 -> { x = r.nextInt(2001) - 1000; z = r.nextInt(2001) - 1000; }
                case 1 -> { x = (r.nextInt(201) - 100) * 16 + r.nextInt(3) - 1; z = (r.nextInt(201) - 100) * 16 + r.nextInt(3) - 1; }
                case 2 -> { x = (r.nextInt(81) - 40) * 128 + r.nextInt(3) - 1; z = (r.nextInt(81) - 40) * 128 + r.nextInt(3) - 1; }
                case 3 -> { x = r.nextInt(60_000_001) - 30_000_000; z = r.nextInt(60_000_001) - 30_000_000; }
                case 4 -> { x = (r.nextBoolean() ? 1 : -1) * (29_999_984 + r.nextInt(17)); z = r.nextInt(400001) - 200000; }
                default -> { x = r.nextInt(20001) - 10000; z = r.nextInt(20001) - 10000; }
            }
            y = switch (r.nextInt(5)) { case 0 -> minY; case 1 -> minY + h - 1; case 2 -> minY + r.nextInt(8); default -> minY + r.nextInt(h); };
            if (r.nextInt(10) == 0) y = r.nextInt(1000) - 500;
            pts[i] = new int[]{x, y, z};
        }
        StringBuilder sb = new StringBuilder("points");
        for (int[] p : pts) sb.append(' ').append(p[0]).append(',').append(p[1]).append(',').append(p[2]);
        o.println(sb);
        List<String> names = new ArrayList<>(List.of("temperature", "vegetation", "continents", "erosion", "depth", "ridges", "chunk_surface_level", "final_density"));
        List<DensityFunction> fs = new ArrayList<>(List.of(rt.temperature(), rt.vegetation(), rt.continents(), rt.erosion(), rt.depth(), rt.ridges(), rt.chunkSurfaceLevel(), rt.finalDensity()));
        if (reg) {
            World.lookup.lookupOrThrow(Registries.DENSITY_FUNCTION).listElements().sorted((a, b) -> a.key().identifier().toString().compareTo(b.key().identifier().toString())).forEach(hd -> {
                names.add(hd.key().identifier().toString());
                fs.add(hd.value());
            });
        }
        for (int k = 0; k < fs.size(); k++) {
            sb = new StringBuilder("f ").append(names.get(k)).append(' ').append(f(fs.get(k).range().min())).append(' ').append(f(fs.get(k).range().max()));
            for (int[] p : pts) sb.append(' ').append(Integer.toHexString(Float.floatToRawIntBits(rs.sampleBlockValueUncached(fs.get(k), p[0], p[1], p[2]))));
            o.println(sb);
        }
        Climate.Sampler cs = rs.createClimateSampler(SamplerContext.EMPTY_UNCACHED);
        for (int[] p : pts) {
            int qx = p[0] >> 2, qy = p[1] >> 2, qz = p[2] >> 2;
            Climate.TargetPoint t = cs.sample(qx, qy, qz);
            o.println("c " + qx + " " + qy + " " + qz + " " + t.temperature() + " " + t.humidity() + " " + t.continentalness() + " " + t.erosion() + " " + t.depth() + " " + t.weirdness());
        }
        o.flush();
    }
}
