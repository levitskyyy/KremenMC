package kremenmc.probe.noise;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.util.Random;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.densityfunction.DensityBuffer;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.synth.BlendedNoise;
import net.minecraft.world.level.levelgen.synth.Noise;
import net.minecraft.world.level.levelgen.synth.NormalNoise;
import net.minecraft.world.level.levelgen.synth.PerlinNoise;
import net.minecraft.world.level.levelgen.synth.SimplexNoise;
import net.minecraft.world.level.levelgen.synth.SmearedPerlinNoise;

// Noise.addToVolume on a pre-filled buffer for each noise kind.
// args: <seed> <volumesPerKind>
public final class VolumeProbe {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Random r = new Random(Long.parseLong(args[0]));
        int vols = Integer.parseInt(args[1]);
        PrintStream o = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false);
        String json = "{\"amplitude_modifiers\":[1.5,0.0,1.0,0.0,0.0,0.0],\"base_amplitude\":1.2453007926713473,\"base_octave\":-10,\"octave_count\":6}";
        NormalNoise nn = NormalNoise.DIRECT_CODEC.parse(JsonOps.INSTANCE, JsonParser.parseString(json)).getOrThrow();
        String[] names = {"perlin 1", "smeared 2", "simplex 5", "normal 3", "fbm 4"};
        Noise[] ns = {new PerlinNoise(new XoroshiroRandomSource(1)), new SmearedPerlinNoise(new XoroshiroRandomSource(2), 0.7),
                new SimplexNoise(new XoroshiroRandomSource(5)), nn.create(new XoroshiroRandomSource(3)), BlendedNoise.createFbm(new XoroshiroRandomSource(4), -7, 4.27, 12.75)};
        for (int k = 0; k < ns.length; k++) {
            o.println("noise " + names[k]);
            for (int t = 0; t < vols; t++) {
                int sx = 1 + r.nextInt(5), sy = 1 + r.nextInt(9), sz = 1 + r.nextInt(5);
                int mx = r.nextInt(60_000_000) - 30_000_000, my = r.nextInt(400) - 64, mz = r.nextInt(60_000_000) - 30_000_000;
                if (t % 2 == 0) { mx = r.nextInt(2000) - 1000; mz = r.nextInt(2000) - 1000; }
                int stx = 1 + r.nextInt(8), sty = 1 + r.nextInt(8), stz = 1 + r.nextInt(8);
                DensityVolume v = new DensityVolume(sx, sy, sz, mx, my, mz, stx, sty, stz);
                double xz = r.nextDouble() * 3, ys = r.nextDouble() * 3;
                float amp = r.nextFloat() * 2 - 0.5f, fill = r.nextFloat() - 0.5f;
                DensityBuffer b = DensityBuffer.createUnpooled(v.size());
                b.fill(fill);
                ns[k].addToVolume(b, v, xz, ys, amp);
                StringBuilder sb = new StringBuilder("vol " + sx + " " + sy + " " + sz + " " + mx + " " + my + " " + mz + " " + stx + " " + sty + " " + stz
                        + " " + Pts.h(xz) + " " + Pts.h(ys) + " " + Pts.h(amp) + " " + Pts.h(fill) + " =");
                for (int ix = 0; ix < sx; ix++) for (int iy = 0; iy < sy; iy++) for (int iz = 0; iz < sz; iz++)
                    sb.append(' ').append(Pts.h(b.get(v.indexUnchecked(ix, iy, iz))));
                o.println(sb);
            }
        }
        o.flush();
    }
}
