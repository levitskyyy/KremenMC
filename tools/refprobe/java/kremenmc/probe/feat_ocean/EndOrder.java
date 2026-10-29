package kremenmc.probe.feat_ocean;

import java.io.PrintStream;
import java.lang.reflect.Field;
import java.util.List;
import java.util.function.Supplier;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.biome.FeatureSorter;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.TheEndBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.chunk.ChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.placement.PlacedFeature;

// EndOrder prints the per-step placed feature order of the vanilla end
// chunk generator (same format as kremenmc.probe.feature.Order).
public final class EndOrder {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        var lookup = VanillaRegistries.createWorldLookup();
        var lists = lookup.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST);
        var settings = lookup.lookupOrThrow(Registries.NOISE_SETTINGS);
        var placed = lookup.lookupOrThrow(Registries.PLACED_FEATURE);
        String[][] dims = {{"the_end", "minecraft:end"}};
        for (String[] d : dims) {
            var src = TheEndBiomeSource.create(lookup.lookupOrThrow(Registries.BIOME));
            var gen = new NoiseBasedChunkGenerator(src, settings.getOrThrow(ResourceKey.create(Registries.NOISE_SETTINGS, Identifier.parse(d[1]))));
            Field f = ChunkGenerator.class.getDeclaredField("featuresPerStep");
            f.setAccessible(true);
            @SuppressWarnings("unchecked")
            List<FeatureSorter.StepFeatureData> steps = ((Supplier<List<FeatureSorter.StepFeatureData>>) f.get(gen)).get();
            out.println("dim " + d[0] + " " + steps.size());
            for (int s = 0; s < steps.size(); s++) {
                var sd = steps.get(s);
                out.print("step " + s);
                for (int i = 0; i < sd.features().size(); i++) {
                    PlacedFeature pf = sd.features().get(i);
                    String key = placed.listElements().filter(h -> h.value() == pf).findFirst().map(h -> h.key().identifier().toString()).orElse("?");
                    int idx = sd.indexMapping().applyAsInt(pf);
                    out.print(" " + key + (idx == i ? "" : "@" + idx));
                }
                out.println();
            }
        }
        out.flush();
        Runtime.getRuntime().halt(0);
    }
}
