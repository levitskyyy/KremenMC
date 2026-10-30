package kremenmc.probe.feat_drip;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayDeque;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;
import net.minecraft.world.level.levelgen.feature.SpeleothemClusterFeature;

// Fn calls single dripstone helpers of the reference on controlled inputs
// with a scripted random source and prints input -> output tables. Lines:
//   h <dev> <bias> <dx> <dz> <density> <height> <f> <g0> <g1> <step>
//     SpeleothemClusterFeature.getSpeleothemHeight with nextFloat = f and
//     nextGaussian swept g0..g1; prints every change of the result.
//   chance <maxEdge> <chanceAtMax> <dx> <dz> <rx> <rz>
//   sh <a> <b> <c> <d>   SpeleothemUtils.getSpeleothemHeight
public final class Fn {
    static class Script implements RandomSource {
        final ArrayDeque<Double> f = new ArrayDeque<>(), g = new ArrayDeque<>(), d = new ArrayDeque<>();
        int calls;
        String log = "";
        public RandomSource fork() { throw new IllegalStateException("fork"); }
        public PositionalRandomFactory forkPositional() { throw new IllegalStateException("forkPositional"); }
        public void setSeed(long s) { throw new IllegalStateException("setSeed"); }
        public int nextInt() { throw new IllegalStateException("nextInt"); }
        public int nextInt(int b) { throw new IllegalStateException("nextInt(b)"); }
        public long nextLong() { throw new IllegalStateException("nextLong"); }
        public boolean nextBoolean() { throw new IllegalStateException("nextBoolean"); }
        public float nextFloat() { calls++; log += "f"; return f.isEmpty() ? 0.5f : f.poll().floatValue(); }
        public double nextDouble() { calls++; log += "d"; return d.isEmpty() ? 0.5 : d.poll(); }
        public double nextGaussian() { calls++; log += "g"; return g.isEmpty() ? 0 : g.poll(); }
    }

    static SpeleothemClusterFeature make(int dev, int bias, int maxEdge, float chanceAtMax) throws Exception {
        var c = SpeleothemClusterFeature.class.getConstructors()[0];
        return (SpeleothemClusterFeature) c.newInstance(null, null, null, 12, null, null, 1, dev, null, null, null, chanceAtMax, maxEdge, bias);
    }

    static int hash(long seed, int dx, int dy, int dz) {
        int h = dx * 73856093 ^ dy * 19349663 ^ dz * 83492791 ^ (int) seed;
        h ^= h >>> 13; h *= 0x5bd1e995; h ^= h >>> 15;
        return h;
    }

    // pbScene: "" is stone on the root's back side and air elsewhere;
    // "hN" mixes air, stone, water, lava and dirt by a hash of seed N.
    static net.minecraft.world.level.block.state.BlockState pbScene(String mode, boolean up, net.minecraft.core.BlockPos bp) {
        if (mode.startsWith("lv") || mode.startsWith("wt")) {
            // lvA/wtA: lava/water at y 0..A, air at A+1..A+2, stone elsewhere
            int top = Integer.parseInt(mode.substring(2));
            if (bp.getY() >= 0 && bp.getY() <= top) return mode.startsWith("lv") ? net.minecraft.world.level.block.Blocks.LAVA.defaultBlockState() : net.minecraft.world.level.block.Blocks.WATER.defaultBlockState();
            if (bp.getY() > top && bp.getY() <= top + 2) return net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
            return net.minecraft.world.level.block.Blocks.STONE.defaultBlockState();
        }
        if (mode.equals("stone")) return net.minecraft.world.level.block.Blocks.STONE.defaultBlockState();
        if (mode.equals("air")) return net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
        if (mode.startsWith("ceil")) return bp.getY() >= Integer.parseInt(mode.substring(4)) ? net.minecraft.world.level.block.Blocks.STONE.defaultBlockState() : net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
        if (mode.startsWith("flr")) return bp.getY() <= Integer.parseInt(mode.substring(3)) ? net.minecraft.world.level.block.Blocks.STONE.defaultBlockState() : net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
        if (mode.startsWith("m")) {
            // m<seed>: stone with 1/4 air holes; pure stone above y 6 and below y -6
            if (Math.abs(bp.getY()) > 6) return net.minecraft.world.level.block.Blocks.STONE.defaultBlockState();
            int k = (hash(Long.parseLong(mode.substring(1)), bp.getX(), bp.getY(), bp.getZ()) >>> 4) & 3;
            return k == 0 ? net.minecraft.world.level.block.Blocks.AIR.defaultBlockState() : net.minecraft.world.level.block.Blocks.STONE.defaultBlockState();
        }
        if (mode.startsWith("z")) {
            if (bp.getY() == 0) return net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
            String[] zs = {"stone", "dirt", "gravel", "sand", "glass", "oak_leaves", "ice", "packed_ice", "snow_block", "dripstone_block",
                "pointed_dripstone", "bedrock", "obsidian", "moss_block", "clay", "calcite", "tuff", "deepslate", "water", "lava"};
            int k = (hash(Long.parseLong(mode.substring(1)), bp.getX(), bp.getY(), bp.getZ()) >>> 4) & 31;
            if (k < 12) return net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
            return BuiltInRegistries.BLOCK.getValue(net.minecraft.resources.Identifier.withDefaultNamespace(zs[k - 12])).defaultBlockState();
        }
        if (mode.startsWith("h")) {
            int k = (hash(Long.parseLong(mode.substring(1)), bp.getX(), bp.getY(), bp.getZ()) >>> 4) & 31;
            if (k < 14) return net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
            if (k < 24) return net.minecraft.world.level.block.Blocks.STONE.defaultBlockState();
            if (k < 27) return net.minecraft.world.level.block.Blocks.WATER.defaultBlockState();
            if (k < 29) return net.minecraft.world.level.block.Blocks.LAVA.defaultBlockState();
            return net.minecraft.world.level.block.Blocks.DIRT.defaultBlockState();
        }
        return (up ? bp.getY() < 0 : bp.getY() > 0) ? net.minecraft.world.level.block.Blocks.STONE.defaultBlockState() : net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
    }

    static net.minecraft.world.level.WorldGenLevel fakeLevel(String scene, boolean up, StringBuilder plog, int surface) {
        java.util.Map<net.minecraft.core.BlockPos, net.minecraft.world.level.block.state.BlockState> m = new java.util.HashMap<>();
        return (net.minecraft.world.level.WorldGenLevel) java.lang.reflect.Proxy.newProxyInstance(Fn.class.getClassLoader(),
            new Class<?>[] {net.minecraft.world.level.WorldGenLevel.class}, (px, mm, ar) -> {
                String n = mm.getName();
                java.util.function.Function<net.minecraft.core.BlockPos, net.minecraft.world.level.block.state.BlockState> get = bp -> m.getOrDefault(bp.immutable(), pbScene(scene, up, bp));
                switch (n) {
                    case "getBlockState": {
                        var bp = (net.minecraft.core.BlockPos) ar[0];
                        plog.append(" G").append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                        return get.apply(bp);
                    }
                    case "getFluidState": return get.apply((net.minecraft.core.BlockPos) ar[0]).getFluidState();
                    case "isStateAtPosition": {
                        @SuppressWarnings("unchecked")
                        var pr = (java.util.function.Predicate<net.minecraft.world.level.block.state.BlockState>) ar[1];
                        var bp = (net.minecraft.core.BlockPos) ar[0];
                        boolean res = pr.test(get.apply(bp));
                        plog.append(" S").append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ()).append(res ? "+" : "-");
                        return res;
                    }
                    case "setBlock": {
                        var bp = ((net.minecraft.core.BlockPos) ar[0]).immutable();
                        m.put(bp, (net.minecraft.world.level.block.state.BlockState) ar[1]);
                        plog.append(" W").append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                        return true;
                    }
                    case "getHeight": plog.append(" H").append(ar == null ? "" : java.util.Arrays.toString(ar).replace(" ", "")); return surface;
                    case "getMinY": return -64;
                    case "getMaxY": return 319;
                    default: throw new IllegalStateException(n);
                }
            });
    }

    static Object field(Object o, String name) throws Exception {
        Field f = null;
        for (Class<?> k = o.getClass(); f == null && k != null; k = k.getSuperclass())
            try { f = k.getDeclaredField(name); } catch (NoSuchFieldException e) { }
        if (f == null) throw new NoSuchFieldException(name);
        f.setAccessible(true);
        return f.get(o);
    }

    static Script floats(StringBuilder plog, String list) {
        String[] vs = list.split(",");
        return new Script() {
            int i;
            @Override public float nextFloat() { calls++; float v = Float.parseFloat(vs[Math.min(i, vs.length - 1)]); i++; plog.append(" f").append(v); return v; }
        };
    }

    public static void main(String[] args) throws Exception {
        var out = System.out;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Method h = SpeleothemClusterFeature.class.getDeclaredMethod("getSpeleothemHeight", RandomSource.class, int.class, int.class, float.class, int.class);
        h.setAccessible(true);
        Method ch = SpeleothemClusterFeature.class.getDeclaredMethod("getChanceOfStalagmiteOrStalactite", int.class, int.class, int.class, int.class);
        ch.setAccessible(true);
        Method sh = Class.forName("net.minecraft.world.level.levelgen.feature.SpeleothemUtils").getDeclaredMethod("getSpeleothemHeight", double.class, double.class, double.class, double.class);
        sh.setAccessible(true);
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            if (line.isBlank() || line.startsWith("#")) continue;
            String[] a = line.trim().split("\\s+");
            switch (a[0]) {
                case "h" -> {
                    var feat = make(Integer.parseInt(a[1]), Integer.parseInt(a[2]), 3, 0.1f);
                    int dx = Integer.parseInt(a[3]), dz = Integer.parseInt(a[4]);
                    float dens = Float.parseFloat(a[5]);
                    int height = Integer.parseInt(a[6]);
                    double fv = Double.parseDouble(a[7]), g0 = Double.parseDouble(a[8]), g1 = Double.parseDouble(a[9]), step = Double.parseDouble(a[10]);
                    StringBuilder sb = new StringBuilder(line).append('\t');
                    Integer last = null;
                    String lastLog = null;
                    for (double gv = g0; gv <= g1 + 1e-12; gv += step) {
                        Script s = new Script();
                        s.f.add(fv);
                        s.g.add(gv);
                        int v = (int) h.invoke(feat, s, dx, dz, dens, height);
                        if (last == null || v != last || !s.log.equals(lastLog)) {
                            sb.append(String.format(" %.6f:%d:%s", gv, v, s.log));
                            last = v;
                            lastLog = s.log;
                        }
                    }
                    out.println(sb);
                }
                case "chance" -> {
                    var feat = make(3, 8, Integer.parseInt(a[1]), Float.parseFloat(a[2]));
                    out.println(line + "\t" + ch.invoke(feat, Integer.parseInt(a[3]), Integer.parseInt(a[4]), Integer.parseInt(a[5]), Integer.parseInt(a[6])));
                }
                case "scan" -> {
                    // scan <range> <pattern from y=-20 upwards>: a air, w water, s stone, l lava
                    int range = Integer.parseInt(a[1]);
                    String pat = a[2];
                    StringBuilder q = new StringBuilder();
                    var rd = (net.minecraft.world.level.LevelSimulatedReader) java.lang.reflect.Proxy.newProxyInstance(Fn.class.getClassLoader(),
                        new Class<?>[] {net.minecraft.world.level.LevelSimulatedReader.class}, (px, m, ar) -> {
                            if (m.getName().equals("isStateAtPosition")) {
                                int y = ((net.minecraft.core.BlockPos) ar[0]).getY();
                                char k = y + 20 >= 0 && y + 20 < pat.length() ? pat.charAt(y + 20) : 's';
                                var st = switch (k) {
                                    case 'a' -> net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
                                    case 'w' -> net.minecraft.world.level.block.Blocks.WATER.defaultBlockState();
                                    case 'l' -> net.minecraft.world.level.block.Blocks.LAVA.defaultBlockState();
                                    default -> net.minecraft.world.level.block.Blocks.STONE.defaultBlockState();
                                };
                                @SuppressWarnings("unchecked")
                                boolean res = ((java.util.function.Predicate<net.minecraft.world.level.block.state.BlockState>) ar[1]).test(st);
                                q.append(' ').append(y).append(res ? "+" : "-");
                                return res;
                            }
                            throw new IllegalStateException(m.getName());
                        });
                    java.util.function.Predicate<net.minecraft.world.level.block.state.BlockState> p1 = st -> st.isAir() || st.is(net.minecraft.world.level.block.Blocks.WATER);
                    java.util.function.Predicate<net.minecraft.world.level.block.state.BlockState> p2 = st -> st.is(net.minecraft.world.level.block.Blocks.STONE);
                    var res = net.minecraft.world.level.levelgen.Column.scan(rd, new net.minecraft.core.BlockPos(0, 0, 0), range, p1, p2);
                    out.println(line + "\t" + res + " |" + q);
                }
                case "ld" -> {
                    // ld <up> <radius> <bluntness> <scale> <r0> <r1> <step>: getHeight and getHeightAtRadius
                    Class<?> k = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$LargeDripstone");
                    var c = k.getDeclaredConstructor(net.minecraft.core.BlockPos.class, boolean.class, int.class, double.class, double.class);
                    c.setAccessible(true);
                    Object d = c.newInstance(net.minecraft.core.BlockPos.ZERO, Boolean.parseBoolean(a[1]), Integer.parseInt(a[2]), Double.parseDouble(a[3]), Double.parseDouble(a[4]));
                    Method gh = k.getDeclaredMethod("getHeight");
                    gh.setAccessible(true);
                    Method ghr = k.getDeclaredMethod("getHeightAtRadius", float.class);
                    ghr.setAccessible(true);
                    StringBuilder sb = new StringBuilder(line).append("\t").append(gh.invoke(d));
                    for (double r = Double.parseDouble(a[5]); r <= Double.parseDouble(a[6]) + 1e-9; r += Double.parseDouble(a[7]))
                        sb.append(' ').append(String.format("%.4f", r)).append(':').append(ghr.invoke(d, (float) r));
                    out.println(sb);
                }
                case "pb" -> {
                    // pb <up> <radius> <bluntness> <scale> <floats,...|const v> [solidSide]: placeBlocks on a fake level
                    // that is stone on the root's back side (y > 0 for a stalactite, y < 0 for a stalagmite) and air elsewhere.
                    boolean up = Boolean.parseBoolean(a[1]);
                    Class<?> k = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$LargeDripstone");
                    var c = k.getDeclaredConstructor(net.minecraft.core.BlockPos.class, boolean.class, int.class, double.class, double.class);
                    c.setAccessible(true);
                    Object d = c.newInstance(net.minecraft.core.BlockPos.ZERO, up, Integer.parseInt(a[2]), Double.parseDouble(a[3]), Double.parseDouble(a[4]));
                    Class<?> wk = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$WindOffsetter");
                    Method nw = wk.getDeclaredMethod("noWind");
                    nw.setAccessible(true);
                    Object wind = nw.invoke(null);
                    Method pbm = k.getDeclaredMethod("placeBlocks", net.minecraft.world.level.WorldGenLevel.class, RandomSource.class, wk);
                    pbm.setAccessible(true);
                    java.util.Map<net.minecraft.core.BlockPos, net.minecraft.world.level.block.state.BlockState> m = new java.util.HashMap<>();
                    StringBuilder plog = new StringBuilder();
                    var lvl = (net.minecraft.world.level.WorldGenLevel) java.lang.reflect.Proxy.newProxyInstance(Fn.class.getClassLoader(),
                        new Class<?>[] {net.minecraft.world.level.WorldGenLevel.class}, (px, mm, ar) -> {
                            String n = mm.getName();
                            java.util.function.Function<net.minecraft.core.BlockPos, net.minecraft.world.level.block.state.BlockState> get = bp -> m.getOrDefault(bp.immutable(), pbScene(a.length > 6 ? a[6] : "", up, bp));
                            switch (n) {
                                case "getBlockState": {
                                    var bp = (net.minecraft.core.BlockPos) ar[0];
                                    plog.append(" G").append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                                    return get.apply(bp);
                                }
                                case "getFluidState": return get.apply((net.minecraft.core.BlockPos) ar[0]).getFluidState();
                                case "isStateAtPosition": {
                                    @SuppressWarnings("unchecked")
                                    var pr = (java.util.function.Predicate<net.minecraft.world.level.block.state.BlockState>) ar[1];
                                    var bp = (net.minecraft.core.BlockPos) ar[0];
                                    boolean res = pr.test(get.apply(bp));
                                    plog.append(" S").append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ()).append(res ? "+" : "-");
                                    return res;
                                }
                                case "setBlock": {
                                    var bp = ((net.minecraft.core.BlockPos) ar[0]).immutable();
                                    m.put(bp, (net.minecraft.world.level.block.state.BlockState) ar[1]);
                                    plog.append(" W").append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                                    return true;
                                }
                                case "getHeight": plog.append(" H").append(ar == null ? "" : java.util.Arrays.toString(ar).replace(" ", "")); return a.length > 7 ? Integer.parseInt(a[7]) : 384;
                                case "getMinY": return -64;
                                case "getMaxY": return 319;
                                default: throw new IllegalStateException(n);
                            }
                        });
                    Script s = new Script() {
                        int i;
                        String[] vs = a[5].split(",");
                        @Override public float nextFloat() { calls++; float v = Float.parseFloat(vs[Math.min(i, vs.length - 1)]); i++; plog.append(" f").append(v); return v; }
                    };
                    pbm.invoke(d, lvl, s, wind);
                    out.println(line + "\t" + plog);
                }
                case "mk" -> {
                    // mk <up> <radius> <floats> <bmin> <bmax> <smin> <smax>: makeDripstone
                    var lk = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature");
                    Method mk = lk.getDeclaredMethod("makeDripstone", net.minecraft.core.BlockPos.class, boolean.class, RandomSource.class, int.class,
                        net.minecraft.util.valueproviders.FloatProvider.class, net.minecraft.util.valueproviders.FloatProvider.class);
                    mk.setAccessible(true);
                    StringBuilder plog = new StringBuilder();
                    var bl = net.minecraft.util.valueproviders.UniformFloat.of(Float.parseFloat(a[4]), Float.parseFloat(a[5]));
                    var sc = net.minecraft.util.valueproviders.UniformFloat.of(Float.parseFloat(a[6]), Float.parseFloat(a[7]));
                    Object d = mk.invoke(null, new net.minecraft.core.BlockPos(0, 5, 0), Boolean.parseBoolean(a[1]), floats(plog, a[3]), Integer.parseInt(a[2]), bl, sc);
                    out.println(line + "\t" + plog + " root=" + field(d, "root") + " radius=" + field(d, "radius") + " bluntness=" + field(d, "bluntness") + " scale=" + field(d, "scale"));
                }
                case "suit" -> {
                    Class<?> k = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$LargeDripstone");
                    var c = k.getDeclaredConstructor(net.minecraft.core.BlockPos.class, boolean.class, int.class, double.class, double.class);
                    c.setAccessible(true);
                    Object d = c.newInstance(net.minecraft.core.BlockPos.ZERO, false, Integer.parseInt(a[1]), Double.parseDouble(a[2]), 1.0);
                    Method sm = k.getDeclaredMethod("isSuitableForWind", int.class, float.class);
                    sm.setAccessible(true);
                    out.println(line + "\t" + sm.invoke(d, Integer.parseInt(a[3]), Float.parseFloat(a[4])));
                }
                case "wind" -> {
                    // wind <originY> <floats> <smin> <smax> <maxOffset> <x,y,z;...>
                    Class<?> wk = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$WindOffsetter");
                    var c = wk.getDeclaredConstructor(int.class, RandomSource.class, net.minecraft.util.valueproviders.FloatProvider.class, int.class);
                    c.setAccessible(true);
                    StringBuilder plog = new StringBuilder();
                    Object w = c.newInstance(Integer.parseInt(a[1]), floats(plog, a[2]), net.minecraft.util.valueproviders.UniformFloat.of(Float.parseFloat(a[3]), Float.parseFloat(a[4])), Integer.parseInt(a[5]));
                    Method off = wk.getDeclaredMethod("offset", net.minecraft.core.BlockPos.class);
                    off.setAccessible(true);
                    plog.append(" speed=").append(field(w, "windSpeed")).append(" max=").append(field(w, "maxOffset"));
                    for (String q : a[6].split(";")) {
                        String[] xyz = q.split(",");
                        var r = (net.minecraft.core.BlockPos) off.invoke(w, new net.minecraft.core.BlockPos(Integer.parseInt(xyz[0]), Integer.parseInt(xyz[1]), Integer.parseInt(xyz[2])));
                        plog.append(' ').append(q).append("->").append(r.getX()).append(',').append(r.getY()).append(',').append(r.getZ());
                    }
                    out.println(line + "\t" + plog);
                }
                case "mb" -> {
                    // mb <up> <radius> <bluntness> <scale> <scene> <rootY>: moveBackUntilBaseIsInsideStoneAndShrinkRadiusIfNecessary, no wind
                    boolean up = Boolean.parseBoolean(a[1]);
                    Class<?> k = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$LargeDripstone");
                    var c = k.getDeclaredConstructor(net.minecraft.core.BlockPos.class, boolean.class, int.class, double.class, double.class);
                    c.setAccessible(true);
                    Object d = c.newInstance(new net.minecraft.core.BlockPos(0, Integer.parseInt(a[6]), 0), up, Integer.parseInt(a[2]), Double.parseDouble(a[3]), Double.parseDouble(a[4]));
                    Class<?> wk = Class.forName("net.minecraft.world.level.levelgen.feature.LargeDripstoneFeature$WindOffsetter");
                    Method nw = wk.getDeclaredMethod("noWind");
                    nw.setAccessible(true);
                    Method mb = k.getDeclaredMethod("moveBackUntilBaseIsInsideStoneAndShrinkRadiusIfNecessary", net.minecraft.world.level.WorldGenLevel.class, wk);
                    mb.setAccessible(true);
                    StringBuilder plog = new StringBuilder();
                    Object res = mb.invoke(d, fakeLevel(a[5], up, plog, 384), nw.invoke(null));
                    out.println(line + "\t" + res + " root=" + field(d, "root") + " radius=" + field(d, "radius") + " |" + plog);
                }
                case "emb" -> {
                    // emb <scene> <y> <radius>: SpeleothemUtils.isCircleMostlyEmbeddedInStone at (0,y,0)
                    Method em = Class.forName("net.minecraft.world.level.levelgen.feature.SpeleothemUtils").getDeclaredMethod("isCircleMostlyEmbeddedInStone",
                        net.minecraft.world.level.WorldGenLevel.class, net.minecraft.core.BlockPos.class, int.class);
                    em.setAccessible(true);
                    StringBuilder plog = new StringBuilder();
                    Object res = em.invoke(null, fakeLevel(a[1], false, plog, 384), new net.minecraft.core.BlockPos(0, Integer.parseInt(a[2]), 0), Integer.parseInt(a[3]));
                    out.println(line + "\t" + res + " |" + plog);
                }
                case "dn" -> {
                    // dn <json> <x0> <x1> <z0> <z1> <y> <step>: BlockStateProvider.getState over a grid (random from seed 1, draws counted)
                    var js = com.google.gson.JsonParser.parseString(a[1]);
                    var prov = net.minecraft.world.level.levelgen.feature.stateproviders.BlockStateProvider.CODEC.parse(com.mojang.serialization.JsonOps.INSTANCE, js).getOrThrow();
                    int x0 = Integer.parseInt(a[2]), x1 = Integer.parseInt(a[3]), z0 = Integer.parseInt(a[4]), z1 = Integer.parseInt(a[5]), y = Integer.parseInt(a[6]), st = Integer.parseInt(a[7]);
                    StringBuilder sb = new StringBuilder();
                    var base = net.minecraft.util.RandomSource.create(1);
                    int[] cnt = {0};
                    RandomSource r = new Script() {
                        @Override public float nextFloat() { cnt[0]++; return base.nextFloat(); }
                        @Override public double nextDouble() { cnt[0]++; return base.nextDouble(); }
                        @Override public int nextInt(int b) { cnt[0]++; return base.nextInt(b); }
                        @Override public int nextInt() { cnt[0]++; return base.nextInt(); }
                        @Override public long nextLong() { cnt[0]++; return base.nextLong(); }
                        @Override public boolean nextBoolean() { cnt[0]++; return base.nextBoolean(); }
                        @Override public double nextGaussian() { cnt[0]++; return base.nextGaussian(); }
                    };
                    for (int x = x0; x <= x1; x += st)
                        for (int z = z0; z <= z1; z += st) {
                            var bs = prov.value().getState(null, r, new net.minecraft.core.BlockPos(x, y, z));
                            sb.append(x).append(',').append(z).append('=').append(BuiltInRegistries.BLOCK.getKey(bs.getBlock()).getPath()).append(' ');
                        }
                    out.println("draws " + cnt[0]);
                    out.println(sb);
                }
                case "dnf" -> {
                    // dnf <json> <x,y,z;...>: slow noise, noise(scale 1), getRandomState(states, f) for f = -1.2..1.2
                    var js = com.google.gson.JsonParser.parseString(a[1]);
                    var prov = (net.minecraft.world.level.levelgen.feature.stateproviders.DualNoiseProvider) net.minecraft.world.level.levelgen.feature.stateproviders.BlockStateProvider.CODEC.parse(com.mojang.serialization.JsonOps.INSTANCE, js).getOrThrow().value();
                    Method sl = net.minecraft.world.level.levelgen.feature.stateproviders.DualNoiseProvider.class.getDeclaredMethod("getSlowNoiseValue", net.minecraft.core.BlockPos.class);
                    sl.setAccessible(true);
                    Method nv = net.minecraft.world.level.levelgen.feature.stateproviders.NoiseBasedStateProvider.class.getDeclaredMethod("getNoiseValue", net.minecraft.core.BlockPos.class, double.class);
                    nv.setAccessible(true);
                    Method rs = net.minecraft.world.level.levelgen.feature.stateproviders.NoiseProvider.class.getDeclaredMethod("getRandomState", java.util.List.class, float.class);
                    rs.setAccessible(true);
                    StringBuilder sb = new StringBuilder();
                    for (String q : a[2].split(";")) {
                        String[] xyz = q.split(",");
                        var bp = new net.minecraft.core.BlockPos(Integer.parseInt(xyz[0]), Integer.parseInt(xyz[1]), Integer.parseInt(xyz[2]));
                        sb.append(q).append(" slow=").append(sl.invoke(prov, bp)).append(" n1=").append(nv.invoke(prov, bp, 1.0)).append(" n01=").append(nv.invoke(prov, bp, 0.1)).append('\n');
                    }
                    @SuppressWarnings("unchecked")
                    var states = (java.util.List<net.minecraft.world.level.block.state.BlockState>) field(prov, "states");
                    for (int i = -120; i <= 120; i++) {
                        float f = i / 100f;
                        var bs = (net.minecraft.world.level.block.state.BlockState) rs.invoke(prov, states, f);
                        sb.append("rs ").append(f).append('=').append(BuiltInRegistries.BLOCK.getKey(bs.getBlock()).getPath()).append('\n');
                    }
                    out.print(sb);
                }
                case "sh" -> out.println(line + "\t" + sh.invoke(null, Double.parseDouble(a[1]), Double.parseDouble(a[2]), Double.parseDouble(a[3]), Double.parseDouble(a[4])));
                default -> out.println(line + "\t?");
            }
        }
        out.flush();
        Runtime.getRuntime().halt(0);
    }
}
