package kremenmc.probe.rng;

import java.io.PrintStream;
import java.io.BufferedOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.Random;
import net.minecraft.SharedConstants;
import net.minecraft.util.Mth;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;
import net.minecraft.world.level.levelgen.RandomSupport;
import net.minecraft.world.level.levelgen.SingleThreadedRandomSource;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;

// Emits a self-describing script of random-source operations and their
// reference results. args: <scriptSeed> <steps>
public final class RngScript {
    static final long[] SEEDS = {0L, 1L, -1L, 42L, Long.MIN_VALUE, Long.MAX_VALUE, 12345678901234L, -98765432109876L,
            0x5DEECE66DL, 0x9E3779B97F4A7C15L, 0x6A09E667F3BCC909L, 1L << 47, 1L << 48, (1L << 48) - 1, Integer.MIN_VALUE, Integer.MAX_VALUE};
    static final int[] BOUNDS = {1, 2, 3, 4, 5, 7, 10, 16, 17, 64, 100, 256, 1000, 4096, 1 << 30, (1 << 30) + 1, Integer.MAX_VALUE, 0x7fff_fff0, 1_500_000_000};
    static final String[] NAMES = {"", "a", "octave_0", "octave_-7", "octave_12", "minecraft:temperature", "minecraft:offset",
            "minecraft:aquifer", "minecraft:ore", "minecraft:terrain", "minecraft:blended_noise", "minecraft:surface", "minecraft:noise/continentalness",
            "été", "世界", "😀 emoji", "with space", "Aa", "BB", "x".repeat(200)};

    final Random r;
    final PrintStream out;
    final List<RandomSource> srcs = new ArrayList<>();
    final List<PositionalRandomFactory> pos = new ArrayList<>();

    RngScript(long seed, PrintStream out) {
        this.r = new Random(seed);
        this.out = out;
    }

    long seed() {
        return switch (r.nextInt(4)) {
            case 0 -> SEEDS[r.nextInt(SEEDS.length)];
            case 1 -> r.nextInt(2001) - 1000;
            default -> r.nextLong();
        };
    }

    int coord() {
        return switch (r.nextInt(6)) {
            case 0 -> r.nextInt(64) - 32;
            case 1 -> r.nextInt(60_000_001) - 30_000_000;
            case 2 -> (r.nextInt(3) - 1) * 30_000_000 + r.nextInt(33) - 16;
            case 3 -> r.nextBoolean() ? Integer.MIN_VALUE + r.nextInt(16) : Integer.MAX_VALUE - r.nextInt(16);
            case 4 -> (r.nextInt(400) - 200) * 16 + r.nextInt(3) - 1;
            default -> r.nextInt();
        };
    }

    int bound() {
        return r.nextInt(3) == 0 ? 1 + r.nextInt(r.nextBoolean() ? 50 : Integer.MAX_VALUE) : BOUNDS[r.nextInt(BOUNDS.length)];
    }

    static String d(double v) {
        return Long.toUnsignedString(Double.doubleToRawLongBits(v), 16);
    }

    static String f(float v) {
        return Integer.toUnsignedString(Float.floatToRawIntBits(v), 16);
    }

    int add(RandomSource s) {
        srcs.add(s);
        return srcs.size() - 1;
    }

    int pick() {
        int n = srcs.size();
        return r.nextInt(4) == 0 ? r.nextInt(n) : n - 1 - r.nextInt(Math.min(n, 4));
    }

    void newSource() {
        int kind = srcs.isEmpty() ? r.nextInt(5) : r.nextInt(12);
        switch (kind) {
            case 0, 1 -> {
                long s = seed();
                out.println("new " + add(new LegacyRandomSource(s)) + " legacy " + s);
            }
            case 2 -> {
                long s = seed();
                out.println("new " + add(new SingleThreadedRandomSource(s)) + " single " + s);
            }
            case 3 -> {
                long s = seed();
                out.println("new " + add(new XoroshiroRandomSource(s)) + " xoro " + s);
            }
            case 4 -> {
                long lo = seed(), hi = r.nextInt(8) == 0 ? 0 : seed();
                if (r.nextInt(20) == 0) lo = 0;
                out.println("new " + add(new XoroshiroRandomSource(lo, hi)) + " xoro128 " + lo + " " + hi);
            }
            case 5, 6 -> {
                boolean x = r.nextBoolean();
                long s = seed();
                RandomSource base = x ? new XoroshiroRandomSource(s) : new LegacyRandomSource(s);
                out.println("new " + add(new WorldgenRandom(base)) + " wg " + (x ? "xoro" : "legacy") + " " + s);
            }
            case 7 -> {
                int i = pick();
                out.println("new " + add(srcs.get(i).fork()) + " fork " + i);
            }
            case 8, 9, 10 -> {
                int i = pick();
                pos.add(srcs.get(i).forkPositional());
                int p = pos.size() - 1;
                out.println("pos " + p + " " + i);
                int m = 1 + r.nextInt(4);
                for (int k = 0; k < m; k++) {
                    switch (r.nextInt(3)) {
                        case 0 -> {
                            int x = coord(), y = coord(), z = coord();
                            if (r.nextBoolean()) y = r.nextInt(448) - 64;
                            out.println("new " + add(pos.get(p).at(x, y, z)) + " at " + p + " " + x + " " + y + " " + z);
                        }
                        case 1 -> {
                            String n = r.nextInt(3) == 0 ? "octave_" + (r.nextInt(40) - 20) : NAMES[r.nextInt(NAMES.length)];
                            out.println("new " + add(pos.get(p).fromHashOf(n)) + " hash " + p + " " + hex(n));
                        }
                        default -> {
                            long s = seed();
                            out.println("new " + add(pos.get(p).fromSeed(s)) + " fseed " + p + " " + s);
                        }
                    }
                }
            }
            default -> {
                int x = coord(), z = coord();
                long s = seed(), salt = r.nextInt(3) == 0 ? 987234911L : seed();
                out.println("new " + add(WorldgenRandom.seedSlimeChunk(x, z, s, salt)) + " slime " + x + " " + z + " " + s + " " + salt);
            }
        }
    }

    static String hex(String s) {
        return "h" + HexFormat.of().formatHex(s.getBytes(StandardCharsets.UTF_8));
    }

    void op() {
        int id = pick();
        RandomSource s = srcs.get(id);
        String p = "op " + id + " ";
        int k = r.nextInt(s instanceof WorldgenRandom ? 22 : 16);
        switch (k) {
            case 0 -> out.println(p + "int = " + s.nextInt());
            case 1, 2 -> {
                int b = bound();
                out.println(p + "intn " + b + " = " + s.nextInt(b));
            }
            case 3 -> {
                int lo = r.nextInt(2000) - 1000, hi = lo + 1 + (r.nextBoolean() ? r.nextInt(100) : r.nextInt(Integer.MAX_VALUE - 2000));
                out.println(p + "intr " + lo + " " + hi + " = " + s.nextInt(lo, hi));
            }
            case 4 -> {
                int lo = r.nextInt(2000) - 1000, hi = lo + r.nextInt(100);
                out.println(p + "intbi " + lo + " " + hi + " = " + s.nextIntBetweenInclusive(lo, hi));
            }
            case 5 -> out.println(p + "long = " + s.nextLong());
            case 6 -> out.println(p + "bool = " + (s.nextBoolean() ? 1 : 0));
            case 7 -> out.println(p + "float = " + f(s.nextFloat()));
            case 8 -> out.println(p + "double = " + d(s.nextDouble()));
            case 9, 10 -> out.println(p + "gauss = " + d(s.nextGaussian()));
            case 11 -> {
                double m = r.nextDouble() * 20 - 10, dv = r.nextDouble() * 5;
                out.println(p + "tri " + d(m) + " " + d(dv) + " = " + d(s.triangle(m, dv)));
            }
            case 12 -> {
                float m = r.nextFloat() * 20 - 10, dv = r.nextFloat() * 5;
                out.println(p + "trif " + f(m) + " " + f(dv) + " = " + f(s.triangle(m, dv)));
            }
            case 13 -> {
                int n = r.nextInt(r.nextBoolean() ? 5 : 300);
                s.consumeCount(n);
                out.println(p + "consume " + n + " = -");
            }
            case 14 -> {
                long sd = seed();
                s.setSeed(sd);
                out.println(p + "setseed " + sd + " = -");
            }
            case 15 -> {
                int n = 1 + r.nextInt(8);
                StringBuilder sb = new StringBuilder();
                for (int i = 0; i < n; i++) sb.append(' ').append(s.nextInt(256));
                out.println(p + "intn256x " + n + " =" + sb);
            }
            default -> {
                WorldgenRandom w = (WorldgenRandom) s;
                long sd = seed();
                switch (k) {
                    case 16 -> {
                        int x = coord() & ~15, z = coord() & ~15;
                        out.println(p + "wgdeco " + sd + " " + x + " " + z + " = " + w.setDecorationSeed(sd, x, z));
                    }
                    case 17 -> {
                        int i = r.nextInt(40), st = r.nextInt(11);
                        w.setFeatureSeed(sd, i, st);
                        out.println(p + "wgfeat " + sd + " " + i + " " + st + " = -");
                    }
                    case 18 -> {
                        int x = coord() >> 4, z = coord() >> 4;
                        w.setLargeFeatureSeed(sd, x, z);
                        out.println(p + "wglarge " + sd + " " + x + " " + z + " = -");
                    }
                    case 19 -> {
                        int x = coord() >> 4, z = coord() >> 4, salt = r.nextInt();
                        w.setLargeFeatureWithSalt(sd, x, z, salt);
                        out.println(p + "wgsalt " + sd + " " + x + " " + z + " " + salt + " = -");
                    }
                    default -> out.println(p + "wgcount = " + w.getCount());
                }
            }
        }
    }

    void statics(int n) {
        for (int i = 0; i < n; i++) {
            long s = seed();
            out.println("mix " + s + " = " + RandomSupport.mixStafford13(s));
            RandomSupport.Seed128bit a = RandomSupport.upgradeSeedTo128bit(s), b = RandomSupport.upgradeSeedTo128bitUnmixed(s);
            out.println("up " + s + " = " + a.seedLo() + " " + a.seedHi());
            out.println("upu " + s + " = " + b.seedLo() + " " + b.seedHi());
            int x = coord(), y = coord(), z = coord();
            out.println("getseed " + x + " " + y + " " + z + " = " + Mth.getSeed(x, y, z));
        }
        for (String nm : NAMES) {
            RandomSupport.Seed128bit h = RandomSupport.seedFromHashOf(nm);
            out.println("md5 " + hex(nm) + " = " + h.seedLo() + " " + h.seedHi());
        }
    }

    void edges() {
        for (String kind : new String[]{"legacy", "xoro", "wg legacy", "wg xoro"}) {
            for (long sd : SEEDS) {
                RandomSource base = kind.endsWith("xoro") ? new XoroshiroRandomSource(sd) : new LegacyRandomSource(sd);
                RandomSource s = kind.startsWith("wg") ? new WorldgenRandom(base) : base;
                int id = add(s);
                out.println("new " + id + " " + kind + " " + sd);
                out.println("op " + id + " gauss = " + d(s.nextGaussian()));
                out.println("op " + id + " setseed " + (sd + 1) + " = -");
                s.setSeed(sd + 1);
                out.println("op " + id + " gauss = " + d(s.nextGaussian()));
                out.println("op " + id + " gauss = " + d(s.nextGaussian()));
                out.println("op " + id + " double = " + d(s.nextDouble()));
                if (s instanceof WorldgenRandom w) out.println("op " + id + " wgcount = " + w.getCount());
                pos.add(s.forkPositional());
                out.println("pos " + (pos.size() - 1) + " " + id);
                for (String n : NAMES) {
                    int h = add(pos.get(pos.size() - 1).fromHashOf(n));
                    out.println("new " + h + " hash " + (pos.size() - 1) + " " + hex(n));
                    out.println("op " + h + " long = " + srcs.get(h).nextLong());
                }
            }
        }
    }

    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        long seed = Long.parseLong(args[0]);
        int steps = Integer.parseInt(args[1]);
        PrintStream out = new PrintStream(new BufferedOutputStream(System.out, 1 << 16), false);
        RngScript s = new RngScript(seed, out);
        s.statics(steps / 20);
        s.edges();
        for (int i = 0; i < steps; i++) {
            if (s.srcs.isEmpty() || s.r.nextInt(6) == 0) s.newSource();
            else s.op();
        }
        out.flush();
    }
}
