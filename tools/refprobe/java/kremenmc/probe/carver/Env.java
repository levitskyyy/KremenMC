package kremenmc.probe.carver;

import java.io.PrintStream;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import net.minecraft.SharedConstants;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.Biomes;
import net.minecraft.world.level.biome.FixedBiomeSource;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.WorldGenerationContext;
import net.minecraft.world.level.levelgen.carver.WorldCarver;

// Shared bootstrap for the carver probes.
final class Env {
    static HolderLookup.Provider lookup;
    static final PrintStream out = new PrintStream(new java.io.BufferedOutputStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), 1 << 16), false);

    static void init() {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        lookup = VanillaRegistries.createWorldLookup();
    }

    static WorldCarver carver(String id) {
        return lookup.lookupOrThrow(Registries.CARVER).getOrThrow(ResourceKey.create(Registries.CARVER, Identifier.parse(id))).value();
    }

    // ctx builds the context vanilla uses for a dimension: overworld or nether settings with the dimension's height.
    static WorldGenerationContext ctx(String dim) {
        String settings = dim.equals("nether") ? "minecraft:nether" : "minecraft:overworld";
        int minY = dim.equals("nether") ? 0 : -64, height = dim.equals("nether") ? 256 : 384;
        var s = lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse(settings)));
        var biome = lookup.lookupOrThrow(Registries.BIOME).getOrThrow(Biomes.PLAINS);
        var gen = new NoiseBasedChunkGenerator(new FixedBiomeSource(biome), s);
        return new WorldGenerationContext(gen, LevelHeightAccessor.create(minY, height));
    }

    static Object call(Object target, Class<?> cls, String name, Object... args) throws Exception {
        for (Method m : cls.getDeclaredMethods()) {
            if (m.getName().equals(name) && m.getParameterCount() == args.length) {
                m.setAccessible(true);
                return m.invoke(target, args);
            }
        }
        throw new NoSuchMethodException(name);
    }

    static Object field(Object target, Class<?> cls, String name) throws Exception {
        Field f = cls.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    static String d(double v) { return Double.toString(v); }
}
