package kremenmc.probe.density;

import net.minecraft.SharedConstants;
import net.minecraft.core.HolderGetter;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.synth.NormalNoise;

// Shared bootstrap: vanilla registries, noise settings and RandomState.
final class World {
    static HolderLookup.Provider lookup;

    static void init() {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        lookup = VanillaRegistries.createWorldLookup();
    }

    static NoiseGeneratorSettings settings(String id) {
        return lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse(id))).value();
    }

    static HolderGetter<NormalNoise> noises() {
        return lookup.lookupOrThrow(Registries.NOISE);
    }

    static RandomState state(String id, long seed) {
        return RandomState.create(noises(), seed, settings(id));
    }
}
