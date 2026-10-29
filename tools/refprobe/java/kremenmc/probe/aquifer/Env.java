package kremenmc.probe.aquifer;

import java.io.PrintStream;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import net.minecraft.SharedConstants;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.Aquifer;
import net.minecraft.world.level.levelgen.Beardifier;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.PositionalRandomFactory;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.blending.Blender;
import net.minecraft.world.level.levelgen.densityfunction.DensityFunction;
import net.minecraft.world.level.levelgen.densityfunction.DensitySampler;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;

// Shared bootstrap: vanilla registries, RandomState and a NoiseChunk built
// the way NoiseBasedChunkGenerator builds one for a chunk (no structures,
// no blending).
final class Env {
    static HolderLookup.Provider lookup;
    static final PrintStream out = new PrintStream(new java.io.BufferedOutputStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), 1 << 16), false);

    static void init() {
        PrintStream o = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        lookup = VanillaRegistries.createWorldLookup();
        System.setOut(o);
    }

    static NoiseGeneratorSettings settings(String id) {
        return lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse(id))).value();
    }

    private static final java.util.Map<String, RandomState> states = new java.util.HashMap<>();

    static RandomState state(String id, long seed) {
        return states.computeIfAbsent(id + " " + seed, k -> RandomState.create(lookup.lookupOrThrow(Registries.NOISE), seed, settings(id)));
    }

    static final java.util.List<String> pickLog = new java.util.ArrayList<>();

    static Aquifer.FluidPicker picker(NoiseGeneratorSettings s, String spec) {
        if (spec.equals("std")) return (Aquifer.FluidPicker) method(NoiseBasedChunkGenerator.class, "createFluidPicker").invoke0(s);
        String[] p = spec.split(":", 4);
        int below = Integer.parseInt(p[0]);
        Aquifer.FluidStatus lava = new Aquifer.FluidStatus(Integer.parseInt(p[1]), state("minecraft:lava"));
        Aquifer.FluidStatus def = new Aquifer.FluidStatus(Integer.parseInt(p[2]), state(p[3]));
        return (x, y, z) -> {
            pickLog.add(x + "," + y + "," + z);
            return y < below ? lava : def;
        };
    }

    static DensityFunction parse(String json) {
        if (json.startsWith("T:")) {
            String[] p = json.split(":", 4);
            return new TableFn(p[1], p[2], Float.parseFloat(p[3]));
        }
        int i = json.indexOf('!');
        if (i > 0) return new LogFn(json.substring(0, i), parse(json.substring(i + 1)));
        var ops = lookup.createSerializationContext(com.mojang.serialization.JsonOps.INSTANCE);
        return DensityFunction.CODEC.parse(ops, com.google.gson.JsonParser.parseString(json)).getOrThrow();
    }

    static Chunk custom(String id, long seed, int cx, int cz, String picker, String[] fns) throws Exception {
        NoiseGeneratorSettings s = settings(id);
        RandomState rs = state(id, seed);
        int minY = s.noiseSettings().minY(), h = s.noiseSettings().height();
        DensityVolume vol = new DensityVolume(16, h, 16, cx << 4, minY, cz << 4);
        Aquifer.FluidPicker fp = picker(s, picker);
        NoiseChunk nc = new NoiseChunk(rs, Beardifier.EMPTY, s, fp, Blender.empty(), vol);
        Aquifer std = nc.aquifer();
        PositionalRandomFactory prf = (PositionalRandomFactory) get(std, std.getClass(), "positionalRandomFactory");
        Aquifer.Config cfg = new Aquifer.Config(parse(fns[0]), parse(fns[2]), parse(fns[3]), parse(fns[4]), parse(fns[1]), parse(fns[5]));
        Aquifer aq = cfg.create(nc.cachingSamplers(), prf, vol, fp);
        return new Chunk(s, rs, nc, cx, cz, aq, cfg);
    }

    record Chunk(NoiseGeneratorSettings settings, RandomState rs, NoiseChunk nc, int cx, int cz, Aquifer custom, Aquifer.Config cfg) {
        Aquifer aquifer() { return custom != null ? custom : nc.aquifer(); }

        DensityFunction fn(String name) {
            Aquifer.Config c = cfg != null ? cfg : settings.aquifers().orElseThrow();
            return switch (name) {
                case "barrier" -> c.barrierNoise();
                case "fluid_level_floodedness" -> c.fluidLevelFloodednessNoise();
                case "fluid_level_spread" -> c.fluidLevelSpreadNoise();
                case "lava" -> c.lavaNoise();
                case "exclusion" -> c.exclusion();
                case "surface_level" -> c.surfaceLevel();
                case "final_density" -> settings.noiseRouter().finalDensity();
                default -> throw new IllegalArgumentException(name);
            };
        }

        float uncached(String name, int x, int y, int z) {
            return rs.sampleBlockValueUncached(fn(name), x, y, z);
        }

        float bound(Aquifer aq, String name, int x, int y, int z) throws Exception {
            String field = switch (name) {
                case "barrier" -> "barrierNoise";
                case "fluid_level_floodedness" -> "fluidLevelFloodednessNoise";
                case "fluid_level_spread" -> "fluidLevelSpreadNoise";
                case "lava" -> "lavaNoise";
                case "exclusion" -> "exclusion";
                case "surface_level" -> "surfaceLevel";
                default -> null;
            };
            if (field == null) return nc.cachingSamplers().sampleValue(fn(name), x, y, z);
            return ((DensitySampler.Bound) get(aq, aq.getClass(), field)).sampleValue(x, y, z);
        }

        PositionalRandomFactory positional(Aquifer aq) throws Exception {
            return (PositionalRandomFactory) get(aq, aq.getClass(), "positionalRandomFactory");
        }
    }

    static Chunk chunk(String id, long seed, int cx, int cz) throws Exception {
        NoiseGeneratorSettings s = settings(id);
        RandomState rs = state(id, seed);
        int minY = s.noiseSettings().minY(), h = s.noiseSettings().height();
        DensityVolume vol = new DensityVolume(16, h, 16, cx << 4, minY, cz << 4);
        Aquifer.FluidPicker fp = picker(s, "std");
        NoiseChunk nc = new NoiseChunk(rs, Beardifier.EMPTY, s, fp, Blender.empty(), vol);
        return new Chunk(s, rs, nc, cx, cz, null, null);
    }

    static BlockState state(String name) {
        return BuiltInRegistries.BLOCK.getValue(Identifier.parse(name)).defaultBlockState();
    }

    static String name(BlockState s) {
        String n = BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString();
        return s == s.getBlock().defaultBlockState() ? n : n + "[" + s + "]";
    }

    static Object get(Object o, Class<?> c, String name) throws ReflectiveOperationException {
        Field f = c.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(o);
    }

    record M(Method m) {
        Object invoke0(Object... a) {
            try {
                return m.invoke(null, a);
            } catch (ReflectiveOperationException e) {
                throw new RuntimeException(e);
            }
        }

        Object invoke(Object t, Object... a) throws ReflectiveOperationException { return m.invoke(t, a); }
    }

    static M method(Class<?> c, String name) {
        for (Method m : c.getDeclaredMethods()) {
            if (m.getName().equals(name)) {
                m.setAccessible(true);
                return new M(m);
            }
        }
        throw new IllegalArgumentException("no method " + name + " in " + c);
    }

    static String d(double v) {
        return Double.toString(v) + "/" + Long.toHexString(Double.doubleToRawLongBits(v));
    }

    static String f(float v) {
        return Float.toString(v) + "/" + Integer.toHexString(Float.floatToRawIntBits(v));
    }
}
