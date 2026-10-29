package kremenmc.probe.beard;

import java.io.PrintStream;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.levelgen.Beardifier;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.TerrainAdjustment;
import net.minecraft.world.level.levelgen.structure.pools.JigsawJunction;
import net.minecraft.world.level.levelgen.structure.pools.StructureTemplatePool;

// BeardProbe evaluates the reference Beardifier as a black box.
//   kernel                         dumps the private kernel table (reflection on runtime state)
//   range                          prints RANGE
//   rigid ADJ x0 y0 z0 x1 y1 z1 GLD  X0 Y0 Z0 X1 Y1 Z1   samples one rigid box over a region
//   junction X Y Z DY PROJ  X0 Y0 Z0 X1 Y1 Z1           samples one junction over a region
// Every sample line is "x y z floatbits value".
public final class BeardProbe {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        switch (args[0]) {
            case "kernel" -> {
                var f = Beardifier.class.getDeclaredField("BEARD_KERNEL");
                f.setAccessible(true);
                float[] k = (float[]) f.get(null);
                out.println("len " + k.length);
                for (int i = 0; i < k.length; i++) out.println(i + " " + Float.floatToRawIntBits(k[i]) + " " + k[i]);
            }
            case "range" -> out.println(Beardifier.RANGE);
            default -> {
                // Items: A X0 Y0 Z0 X1 Y1 Z1 (affected box) | R ADJ x0 y0 z0 x1 y1 z1 GLD | J X Y Z DY PROJ | Q X0 Y0 Z0 X1 Y1 Z1
                List<Beardifier.Rigid> rig = new ArrayList<>();
                List<JigsawJunction> jun = new ArrayList<>();
                int[] r = null;
                int i = 0;
                BoundingBox aff = new BoundingBox(-100000, -1000, -100000, 100000, 1000, 100000);
                if (args[0].equals("rigid")) args[0] = "R";
                if (args[0].equals("junction")) args[0] = "J";
                while (i < args.length) {
                    switch (args[i++]) {
                        case "R" -> {
                            TerrainAdjustment adj = TerrainAdjustment.valueOf(args[i++]);
                            int[] b = ints(args, i, 7);
                            i += 7;
                            rig.add(new Beardifier.Rigid(new BoundingBox(b[0], b[1], b[2], b[3], b[4], b[5]), adj, b[6]));
                            if (i < args.length && args[i].matches("-?\\d+")) {
                                r = ints(args, i, 6);
                                i += 6;
                            }
                        }
                        case "J" -> {
                            int[] j = ints(args, i, 4);
                            i += 4;
                            jun.add(new JigsawJunction(j[0], j[1], j[2], j[3], StructureTemplatePool.Projection.valueOf(args[i++])));
                            if (i < args.length && args[i].matches("-?\\d+")) {
                                r = ints(args, i, 6);
                                i += 6;
                            }
                        }
                        case "A" -> {
                            int[] b = ints(args, i, 6);
                            i += 6;
                            aff = new BoundingBox(b[0], b[1], b[2], b[3], b[4], b[5]);
                        }
                        case "Q" -> {
                            r = ints(args, i, 6);
                            i += 6;
                        }
                        default -> throw new IllegalArgumentException(args[i - 1]);
                    }
                }
                var bd = new Beardifier(rig, jun, aff);
                for (int x = r[0]; x <= r[3]; x++)
                    for (int y = r[1]; y <= r[4]; y++)
                        for (int z = r[2]; z <= r[5]; z++) {
                            float v = bd.sampleValue(SamplerContext.EMPTY_UNCACHED, x, y, z);
                            if (v != 0) out.println(x + " " + y + " " + z + " " + Float.floatToRawIntBits(v) + " " + v);
                        }
            }
        }
        out.flush();
        Runtime.getRuntime().halt(0);
    }

    static int[] ints(String[] a, int from, int n) {
        int[] r = new int[n];
        for (int k = 0; k < n; k++) r[k] = Integer.parseInt(a[from + k]);
        return r;
    }
}
