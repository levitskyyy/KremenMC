package kremenmc.probe.feature;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.storage.LevelStorageSource;

// Survive prints, for each block given as an argument (default state), the
// blocks (default states) it survives on in open air, then again with water
// next to the support block. Black-box: only BlockState.canSurvive is called.
public final class Survive {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featureworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                BlockPos pos = new BlockPos(0, 200, 0), below = pos.below(), side = below.east();
                for (String id : args) {
                    var key = Identifier.parse(id);
                    if (!BuiltInRegistries.BLOCK.containsKey(key)) continue;
                    BlockState plant = BuiltInRegistries.BLOCK.getValue(key).defaultBlockState();
                    for (int pass = 0; pass < 2; pass++) {
                        StringBuilder sb = new StringBuilder(pass == 0 ? "air " : "water ").append(id);
                        level.setBlock(side, pass == 0 ? Blocks.AIR.defaultBlockState() : Blocks.WATER.defaultBlockState(), 2 | 16);
                        for (Block b : BuiltInRegistries.BLOCK) {
                            level.setBlock(below, b.defaultBlockState(), 2 | 16);
                            if (plant.canSurvive(level, pos)) sb.append(' ').append(BuiltInRegistries.BLOCK.getKey(b));
                        }
                        out.println(sb);
                    }
                }
                f.complete(null);
            } catch (Throwable t) {
                f.completeExceptionally(t);
            }
        });
        f.get();
        out.flush();
        Runtime.getRuntime().halt(0);
    }
}
