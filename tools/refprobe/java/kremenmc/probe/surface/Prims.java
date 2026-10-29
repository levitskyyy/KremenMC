package kremenmc.probe.surface;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import net.minecraft.core.Holder;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeManager;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;
import net.minecraft.world.level.levelgen.material.MaterialSystem;
import net.minecraft.world.level.levelgen.synth.Noise;

// Prims prints MaterialSystem primitives observed through reflection: surface
// depth and secondary per column, the clay band table and getBand, values of
// the system's noises, its positional random factory, and which quart the
// biome manager zoom picks for block positions.
// Args: <settings> <dim> <seed> <points> <pointSeed>
public final class Prims {
    public static void main(String[] args) throws Exception {
        Env.boot();
        PrintStream o = new PrintStream(new BufferedOutputStream(Env.out, 1 << 16), false);
        try {
            Gen g = new Gen(args[0], args[1], Long.parseLong(args[2]));
            int n = Integer.parseInt(args[3]);
            Random r = new Random(Long.parseLong(args[4]));
            MaterialSystem ms = g.rs.surfaceSystem();
            var depth = Env.method(MaterialSystem.class, "getSurfaceDepth");
            var sec = Env.method(MaterialSystem.class, "getSurfaceSecondary");
            var band = Env.method(MaterialSystem.class, "getBand");
            int[][] pts = new int[n][];
            for (int i = 0; i < n; i++) {
                int x, z;
                switch (i % 4) {
                    case 0 -> { x = r.nextInt(2001) - 1000; z = r.nextInt(2001) - 1000; }
                    case 1 -> { x = r.nextInt(60_000_001) - 30_000_000; z = r.nextInt(60_000_001) - 30_000_000; }
                    case 2 -> { x = r.nextInt(64) - 32; z = r.nextInt(64) - 32; }
                    default -> { x = r.nextInt(200001) - 100000; z = r.nextInt(200001) - 100000; }
                }
                pts[i] = new int[] {x, g.minY() + r.nextInt(g.height() + 64) - 32, z};
            }
            o.println("obf " + BiomeManager.obfuscateSeed(g.seed));
            for (int[] p : pts) {
                o.println("sd " + p[0] + " " + p[2] + " " + depth.invoke(ms, p[0], p[2]) + " " + Double.doubleToRawLongBits((double) sec.invoke(ms, p[0], p[2])));
                o.println("band " + p[0] + " " + p[1] + " " + p[2] + " " + Trace.stateString((BlockState) band.invoke(ms, p[0], p[1], p[2])));
            }
            BlockState[] bands = (BlockState[]) Env.get(ms, MaterialSystem.class, "clayBands");
            StringBuilder sb = new StringBuilder("bands " + bands.length);
            for (BlockState b : bands) sb.append(' ').append(Trace.stateString(b));
            o.println(sb);
            for (String f : new String[] {"clayBandsOffsetNoise", "badlandsPillarNoise", "badlandsPillarRoofNoise", "badlandsSurfaceNoise", "icebergPillarNoise", "icebergPillarRoofNoise", "icebergSurfaceNoise", "surfaceNoise", "surfaceSecondaryNoise"}) {
                Noise nz = (Noise) Env.get(ms, MaterialSystem.class, f);
                sb = new StringBuilder("noise " + f);
                for (int i = 0; i < 8; i++) {
                    int[] p = pts[i];
                    sb.append(' ').append(Integer.toHexString(Float.floatToRawIntBits(nz.get(p[0] * 0.37, p[1] * 0.5, p[2] * 1.3))));
                    sb.append(' ').append(Integer.toHexString(Float.floatToRawIntBits(nz.get(p[0] * 0.37, p[2] * 1.3))));
                }
                o.println(sb);
            }
            PositionalRandomFactory nr = (PositionalRandomFactory) Env.get(ms, MaterialSystem.class, "noiseRandom");
            o.println("noiseRandom " + nr.fromHashOf("a").nextLong() + " " + nr.at(1, 2, 3).nextLong() + " " + nr.fromSeed(5).nextLong());
            for (String id : new String[] {"minecraft:bedrock_floor", "minecraft:deepslate"}) {
                PositionalRandomFactory f = g.rs.getOrCreateRandomFactory(net.minecraft.resources.Identifier.parse(id));
                o.println("factory " + id + " " + f.at(1, 2, 3).nextLong() + " " + f.at(-7, -60, 9).nextFloat());
            }
            List<int[]> calls = new ArrayList<>();
            Holder<Biome> any = g.src.possibleBiomes().iterator().next();
            BiomeManager bm = new BiomeManager((x, y, z) -> {
                calls.add(new int[] {x, y, z});
                return any;
            }, BiomeManager.obfuscateSeed(g.seed));
            for (int[] p : pts) {
                calls.clear();
                bm.getBiome(p[0], p[1], p[2]);
                sb = new StringBuilder("zoom " + p[0] + " " + p[1] + " " + p[2]);
                for (int[] c : calls) sb.append(' ').append(c[0]).append(',').append(c[1]).append(',').append(c[2]);
                o.println(sb);
            }
        } catch (Throwable t) {
            t.printStackTrace(o);
        }
        o.flush();
        Env.exit();
    }
}
