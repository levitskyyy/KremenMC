package kremenmc.probe.carver;

import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.util.Mth;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.chunk.CarverOutput;
import net.minecraft.world.level.levelgen.carver.CaveWorldCarver;
import net.minecraft.world.level.levelgen.carver.WorldCarver;

// Fns prints golden vectors for the carver building blocks. Doubles are
// printed as raw hex bits. Args: <what> [n]
//   sincos: Mth.sin/cos on random and grid inputs
//   reach:  WorldCarver.canReach on random inputs
//   ellipsoid: WorldCarver.carveEllipsoid, every checker call and carve
//   caveskip: CaveWorldCarver.shouldSkip(dx, dy, dz, floor)
public final class Fns {
    static String h(double v) { return Long.toHexString(Double.doubleToRawLongBits(v)); }
    static String hf(float v) { return Integer.toHexString(Float.floatToRawIntBits(v)); }

    // emit prints the event lines of a case, or their CRC32 and count.
    static void emit(List<String> lines, boolean verbose) {
        if (verbose) {
            for (String l : lines) Env.out.println(l);
            return;
        }
        var crc = new java.util.zip.CRC32();
        for (String l : lines) crc.update((l + "\n").getBytes());
        Env.out.println("= " + Long.toHexString(crc.getValue()) + " " + lines.size());
    }

    public static void main(String[] a) throws Exception {
        Env.init();
        var o = Env.out;
        int n = a.length > 1 ? Integer.parseInt(a[1]) : 1000;
        boolean verbose = a.length > 2 && a[2].equals("v");
        Random rnd = new Random(12345);
        switch (a[0]) {
            case "sincos" -> {
                for (int i = 0; i < n; i++) {
                    double x = switch (i % 4) {
                        case 0 -> (rnd.nextDouble() - 0.5) * 40;
                        case 1 -> (float) ((rnd.nextDouble() - 0.5) * 20);
                        case 2 -> i * Math.PI / 4096;
                        default -> (rnd.nextInt(1 << 20) - (1 << 19)) / 10430.378 + (rnd.nextInt(3) - 1) * 1e-9;
                    };
                    o.println(h(x) + " " + hf(Mth.sin(x)) + " " + hf(Mth.cos(x)));
                }
            }
            case "reach" -> {
                for (int i = 0; i < n; i++) {
                    var cp = new ChunkPos(rnd.nextInt(21) - 10, rnd.nextInt(21) - 10);
                    double x = cp.getMinBlockX() + (rnd.nextDouble() - 0.5) * 300, z = cp.getMinBlockZ() + (rnd.nextDouble() - 0.5) * 300;
                    int dist = 1 + rnd.nextInt(200), step = rnd.nextInt(dist + 1);
                    float th = rnd.nextFloat() * 8;
                    o.println(cp.x() + " " + cp.z() + " " + h(x) + " " + h(z) + " " + step + " " + dist + " " + hf(th) + " " + WorldCarver.canReach(cp, x, z, step, dist, th));
                }
            }
            case "ellipsoid" -> {
                for (int i = 0; i < n; i++) {
                    var cp = new ChunkPos(rnd.nextInt(5) - 2, rnd.nextInt(5) - 2);
                    double x = cp.getMinBlockX() + (rnd.nextDouble() - 0.25) * 30, z = cp.getMinBlockZ() + (rnd.nextDouble() - 0.25) * 30;
                    double y = (rnd.nextDouble() - 0.5) * 150;
                    double hr = rnd.nextDouble() * 12, vr = rnd.nextDouble() * 12;
                    if (i % 7 == 0) { x = Math.floor(x) + 0.5; hr = Math.floor(hr) + (i % 2) * 0.5; }
                    int minY = -64 + rnd.nextInt(3) * 30, maxY = minY + 100;
                    int skipMod = rnd.nextInt(4);
                    o.println("case " + cp.x() + " " + cp.z() + " " + h(x) + " " + h(y) + " " + h(z) + " " + h(hr) + " " + h(vr) + " " + minY + " " + maxY + " " + skipMod);
                    List<String> lines = new ArrayList<>();
                    CarverOutput out = new CarverOutput() {
                        public void carve(int px, int py, int pz) { lines.add("c " + px + " " + py + " " + pz); }
                        public int minY() { return minY; }
                        public int maxY() { return maxY; }
                    };
                    int[] k = {0};
                    WorldCarver.carveEllipsoid(cp, x, y, z, hr, vr, out, (dx, dy, dz, py) -> {
                        lines.add("s " + h(dx) + " " + h(dy) + " " + h(dz) + " " + py);
                        return skipMod != 0 && (k[0]++ % (skipMod + 1)) == 0;
                    });
                    emit(lines, verbose);
                }
            }
            case "caveskip" -> {
                for (int i = 0; i < n; i++) {
                    double dx = (rnd.nextDouble() - 0.5) * 2.4, dy = (rnd.nextDouble() - 0.5) * 2.4, dz = (rnd.nextDouble() - 0.5) * 2.4;
                    double fl = (rnd.nextDouble() - 0.5) * 2.4;
                    if (i % 5 == 0) dy = fl;
                    if (i % 11 == 0) { dx = 0.6; dy = 0.8; dz = 0; }
                    o.println(h(dx) + " " + h(dy) + " " + h(dz) + " " + h(fl) + " " + Env.call(null, CaveWorldCarver.class, "shouldSkip", dx, dy, dz, fl));
                }
            }
            case "providers" -> {
                String[] floats = {
                    "{\"type\":\"minecraft:uniform\",\"max_exclusive\":1.4,\"min_inclusive\":0.7}",
                    "{\"type\":\"minecraft:uniform\",\"max_exclusive\":-0.4,\"min_inclusive\":-1.0}",
                    "{\"type\":\"minecraft:uniform\",\"max_exclusive\":0.125,\"min_inclusive\":-0.125}",
                    "{\"type\":\"minecraft:trapezoid\",\"max\":3.0,\"min\":0.0,\"plateau\":1.0}",
                    "{\"type\":\"minecraft:trapezoid\",\"max\":6.0,\"min\":0.0,\"plateau\":2.0}",
                    "{\"type\":\"minecraft:trapezoid\",\"max\":5.5,\"min\":-1.3,\"plateau\":0.7}",
                    "0.37",
                };
                for (String js : floats) {
                    var p = net.minecraft.util.valueproviders.FloatProviders.CODEC.parse(com.mojang.serialization.JsonOps.INSTANCE, com.google.gson.JsonParser.parseString(js)).getOrThrow();
                    var r = new net.minecraft.world.level.levelgen.LegacyRandomSource(rnd.nextLong());
                    StringBuilder sb = new StringBuilder("float " + js);
                    for (int i = 0; i < n; i++) sb.append(' ').append(hf(p.sample(r)));
                    sb.append(" next ").append(r.nextInt());
                    o.println(sb);
                }
                String[] ints = {
                    "{\"type\":\"minecraft:very_biased_to_bottom\",\"max_inclusive\":14,\"min_inclusive\":0}",
                    "{\"type\":\"minecraft:very_biased_to_bottom\",\"max_inclusive\":9,\"min_inclusive\":0}",
                    "{\"type\":\"minecraft:very_biased_to_bottom\",\"max_inclusive\":20,\"min_inclusive\":3}",
                };
                for (String js : ints) {
                    var p = net.minecraft.util.valueproviders.IntProviders.CODEC.parse(com.mojang.serialization.JsonOps.INSTANCE, com.google.gson.JsonParser.parseString(js)).getOrThrow();
                    var r = new net.minecraft.world.level.levelgen.LegacyRandomSource(rnd.nextLong());
                    StringBuilder sb = new StringBuilder("int " + js);
                    for (int i = 0; i < n; i++) sb.append(' ').append(p.sample(r));
                    sb.append(" next ").append(r.nextInt());
                    o.println(sb);
                }
                String[] heights = {
                    "{\"type\":\"minecraft:uniform\",\"max_inclusive\":{\"absolute\":180},\"min_inclusive\":{\"above_bottom\":8}}",
                    "{\"type\":\"minecraft:uniform\",\"max_inclusive\":{\"below_top\":1},\"min_inclusive\":{\"absolute\":0}}",
                    "{\"type\":\"minecraft:uniform\",\"max_inclusive\":{\"absolute\":67},\"min_inclusive\":{\"absolute\":10}}",
                };
                for (String dim : new String[] {"overworld", "nether"}) {
                    var ctx = Env.ctx(dim);
                    for (String js : heights) {
                        var p = net.minecraft.world.level.levelgen.heightproviders.HeightProvider.CODEC.parse(com.mojang.serialization.JsonOps.INSTANCE, com.google.gson.JsonParser.parseString(js)).getOrThrow();
                        var r = new net.minecraft.world.level.levelgen.LegacyRandomSource(rnd.nextLong());
                        StringBuilder sb = new StringBuilder("height " + dim + " " + js);
                        for (int i = 0; i < n; i++) sb.append(' ').append(p.sample(r, ctx));
                        sb.append(" next ").append(r.nextInt());
                        o.println(sb);
                    }
                }
            }
            case "canyon" -> {
                // canyon <n> [v] <carver json file> <dim>: initWidthFactors, shouldSkip and updateVerticalRadius.
                var c = (net.minecraft.world.level.levelgen.carver.CanyonWorldCarver) Trace.parse(a[3]);
                var ctx = Env.ctx(a[4]);
                var cls = net.minecraft.world.level.levelgen.carver.CanyonWorldCarver.class;
                for (int i = 0; i < 20; i++) {
                    long seed = rnd.nextLong();
                    float[] w = (float[]) Env.call(c, cls, "initWidthFactors", ctx, new net.minecraft.world.level.levelgen.LegacyRandomSource(seed));
                    List<String> l = new ArrayList<>();
                    for (float f : w) l.add(hf(f));
                    o.println("width " + seed + " " + w.length);
                    emit(l, verbose);
                    if (i > 0) continue;
                    for (int k = 0; k < n; k++) {
                        double dx = (rnd.nextDouble() - 0.5) * 2.4, dy = (rnd.nextDouble() - 0.5) * 2.4, dz = (rnd.nextDouble() - 0.5) * 2.4;
                        int y = ctx.getMinGenY() + rnd.nextInt(ctx.getGenDepth() + 2) - 1;
                        Object res;
                        try {
                            res = Env.call(c, cls, "shouldSkip", ctx, w, dx, dy, dz, y);
                        } catch (java.lang.reflect.InvocationTargetException e) {
                            res = "exception";
                        }
                        o.println("skip " + h(dx) + " " + h(dy) + " " + h(dz) + " " + y + " " + res);
                    }
                }
                for (int k = 0; k < n; k++) {
                    long seed = rnd.nextLong();
                    double vr = rnd.nextDouble() * 20;
                    int dist = 1 + rnd.nextInt(150);
                    float step = rnd.nextInt(dist);
                    var r = new net.minecraft.world.level.levelgen.LegacyRandomSource(seed);
                    double res = (double) Env.call(c, cls, "updateVerticalRadius", r, vr, (float) dist, step);
                    o.println("vr " + seed + " " + h(vr) + " " + dist + " " + hf(step) + " " + h(res) + " " + r.nextInt());
                }
            }
            case "biomeq" -> {
                var s = Env.lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse("minecraft:overworld")));
                var biome = Env.lookup.lookupOrThrow(Registries.BIOME).getOrThrow(net.minecraft.world.level.biome.Biomes.PLAINS);
                var gen = new net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator(new net.minecraft.world.level.biome.FixedBiomeSource(biome), s);
                for (int[] c : new int[][] {{0, 0}, {5, -7}, {-3, 2}}) {
                    net.minecraft.world.level.biome.BiomeResolver res = (x, y, z) -> {
                        o.println("query " + c[0] + " " + c[1] + " -> " + x + " " + y + " " + z);
                        return biome;
                    };
                    Env.call(gen, net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator.class, "getBiomeGenerationSettingsForCarver", res, new ChunkPos(c[0], c[1]));
                }
                var cave = Env.carver("minecraft:cave");
                for (float f : new float[] {0.15f, Math.nextUp(0.15f), Math.nextDown(0.15f)}) {
                    var stub = new LogRandom(new net.minecraft.world.level.levelgen.LegacyRandomSource(0)) {
                        public float nextFloat() { return f; }
                    };
                    o.println("isStart " + f + " " + cave.isStartChunk(stub));
                }
            }
            default -> throw new IllegalArgumentException(a[0]);
        }
        o.flush();
    }
}
