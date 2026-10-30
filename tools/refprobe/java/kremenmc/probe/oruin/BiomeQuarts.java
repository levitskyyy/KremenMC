package kremenmc.probe.oruin;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.storage.LevelStorageSource;

// BiomeQuarts prints the biomes MultiNoiseBiomeSource.findBiomeHorizontal
// tests for a square search (radius in quarts, step 1, block y = qy*4), in
// search order, one name per line after "C x z". Args: seed qy
// radiusQuarts x,z ...
public final class BiomeQuarts {
    public static void main(String[] args) throws Exception {
        var out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "biomequartsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        long seed = Long.parseLong(args[0]);
        int qy = Integer.parseInt(args[1]), qr = Integer.parseInt(args[2]);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var ra = server.overworld().registryAccess();
                var settings = ra.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD);
                var preset = ra.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD);
                var bs = MultiNoiseBiomeSource.createFromPreset(preset);
                var rs = RandomState.create(ra.lookupOrThrow(Registries.NOISE), seed, settings.value());
                StringBuilder sb = new StringBuilder();
                for (int a = 3; a < args.length; a++) {
                    String[] p = args[a].split(",");
                    int x = Integer.parseInt(p[0]), z = Integer.parseInt(p[1]);
                    sb.append("C ").append(x).append(' ').append(z).append('\n');
                    // The predicate sees every quart in search order; it
                    // never matches, so the whole square is scanned.
                    bs.findBiomeHorizontal(x, qy << 2, z, qr << 2, 1, h -> { sb.append(h.unwrapKey().get().identifier().getPath()).append('\n'); return false; },
                        net.minecraft.util.RandomSource.create(0), false, rs);
                }
                out.print(sb);
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { out.flush(); Runtime.getRuntime().halt(0); }
    }
}
