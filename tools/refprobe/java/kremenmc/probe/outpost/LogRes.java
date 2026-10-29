package kremenmc.probe.outpost;

import java.util.stream.Stream;
import net.minecraft.core.Holder;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeResolver;
import net.minecraft.world.level.biome.BiomeSource;
import net.minecraft.world.level.biome.Climate;

// LogRes forwards to a biome source; resolvers it creates log every quart
// query into log.
final class LogRes extends BiomeSource {
    final BiomeSource d;
    static final StringBuilder log = new StringBuilder();

    LogRes(BiomeSource d) { this.d = d; }

    protected com.mojang.serialization.MapCodec<? extends BiomeSource> codec() { return null; }
    protected Stream<Holder<Biome>> collectPossibleBiomes() { return d.possibleBiomes().stream(); }
    public BiomeResolver createResolver(Climate.Sampler s) {
        BiomeResolver r = d.createResolver(s);
        return (x, y, z) -> {
            var b = r.getNoiseBiome(x, y, z);
            log.append(" Q").append(x).append(',').append(y).append(',').append(z).append('=').append(b.unwrapKey().get().identifier().getPath());
            return b;
        };
    }
}
