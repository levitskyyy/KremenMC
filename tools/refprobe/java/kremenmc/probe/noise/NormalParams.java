package kremenmc.probe.noise;

import com.google.gson.JsonElement;
import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.BufferedOutputStream;
import java.io.InputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import java.util.zip.ZipEntry;
import java.util.zip.ZipFile;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.levelgen.synth.Noise;
import net.minecraft.world.level.levelgen.synth.NormalNoise;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;

// NormalNoise parameter handling: decoded parameters, octave list,
// normalization factor and range for vanilla, random and parity inputs.
// args: <seed> <nRandom> <jarPath> <samplesPerNoise>
public final class NormalParams {
    static final long[] SEEDS = {0L, -1L, 1L, Long.MIN_VALUE, Long.MAX_VALUE, 42L, 12345L};

    static void sample(PrintStream o, Noise n, Pts g, int samples) {
        for (int i = 0; i < samples; i++) {
            double x = g.c(), y = g.y(), z = g.c();
            o.println("s " + Pts.h(x) + " " + Pts.h(y) + " " + Pts.h(z) + " = " + Pts.h(n.get(x, y, z)) + " " + Pts.h(n.get(x, z)));
        }
    }

    static void emit(PrintStream o, NormalNoise nn, Random r, int samples) {
        long sd = r.nextInt(3) == 0 ? SEEDS[r.nextInt(SEEDS.length)] : r.nextLong();
        Pts g = new Pts(r.nextLong());
        StringBuilder sb = new StringBuilder("seed " + sd + "\n");
        Noise n = nn.create(new XoroshiroRandomSource(sd));
        Refl.describe(sb, n, 0);
        o.print(sb);
        sample(o, n, g, samples);
        try {
            n = nn.createForLegacyNetherBiome(new LegacyRandomSource(sd));
        } catch (Exception ex) {
            o.println("legacy error");
            return;
        }
        sb = new StringBuilder("legacy\n");
        Refl.describe(sb, n, 0);
        o.print(sb);
        sample(o, n, g, samples / 2);
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Random r = new Random(Long.parseLong(args[0]));
        int n = Integer.parseInt(args[1]);
        int samples = Integer.parseInt(args[3]);
        PrintStream o = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false, StandardCharsets.UTF_8);
        List<String> jsons = new ArrayList<>();
        try (ZipFile z = new ZipFile(args[2])) {
            for (ZipEntry e : z.stream().toList()) {
                if (e.getName().startsWith("data/minecraft/worldgen/noise/") && e.getName().endsWith(".json")) {
                    try (InputStream in = z.getInputStream(e)) {
                        jsons.add(new String(in.readAllBytes(), StandardCharsets.UTF_8).replaceAll("\\s", ""));
                    }
                }
            }
        }
        for (int i = 0; i < n; i++) {
            StringBuilder j = new StringBuilder("{");
            int count = 1 + r.nextInt(r.nextBoolean() ? 6 : 16);
            j.append("\"base_octave\":").append(r.nextInt(30) - 20);
            if (r.nextInt(5) > 0) j.append(",\"octave_count\":").append(count); else count = 1;
            if (r.nextInt(4) > 0) j.append(",\"base_amplitude\":").append(r.nextInt(4) == 0 ? (double) (1 + r.nextInt(4)) : r.nextDouble() * 3);
            if (r.nextInt(3) > 0) {
                j.append(",\"amplitude_modifiers\":[");
                for (int k = 0; k < count; k++) {
                    double m = switch (r.nextInt(5)) { case 0 -> 0.0; case 1 -> 1.0; case 2 -> (double) r.nextInt(4); default -> r.nextDouble() * 3; };
                    j.append(k > 0 ? "," : "").append(m);
                }
                j.append("]");
            }
            switch (r.nextInt(5)) {
                case 0 -> j.append(",\"normalize\":false");
                case 1 -> j.append(",\"normalize\":\"legacy\"");
                case 2 -> j.append(",\"normalize\":true");
                default -> {}
            }
            jsons.add(j.append("}").toString());
        }
        for (String js : jsons) {
            o.println("json " + js);
            JsonElement e = JsonParser.parseString(js);
            var res = NormalNoise.DIRECT_CODEC.parse(JsonOps.INSTANCE, e);
            if (res.isError()) {
                o.println("error");
                continue;
            }
            NormalNoise nn = res.getOrThrow();
            o.print(Refl.normal(nn));
            emit(o, nn, r, samples);
        }
        for (int i = 0; i < n; i++) {
            int first = r.nextInt(24) - 18;
            int cnt = 1 + r.nextInt(r.nextBoolean() ? 4 : 12);
            double[] amps = new double[cnt];
            for (int k = 0; k < cnt; k++) amps[k] = switch (r.nextInt(5)) { case 0, 1 -> 0.0; case 2 -> 1.0; case 3 -> r.nextInt(4); default -> r.nextDouble() * 2; };
            StringBuilder line = new StringBuilder("parity " + first);
            for (double a : amps) line.append(' ').append(Pts.h(a));
            o.println(line);
            NormalNoise nn;
            try {
                nn = NormalNoise.createParity(first, amps);
            } catch (Exception ex) {
                o.println("error");
                continue;
            }
            o.print(Refl.normal(nn));
            emit(o, nn, r, samples);
        }
        o.flush();
    }
}
