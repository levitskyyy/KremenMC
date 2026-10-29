package kremenmc.probe.feat_ore;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.RegistryOps;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.placement.PlacementContext;
import net.minecraft.world.level.levelgen.placement.PlacementModifier;
import net.minecraft.world.level.storage.LevelStorageSource;

// Place runs placement modifiers with a real PlacementContext on the flat
// test world (bedrock -64, dirt -63..-62, grass_block -61, air above).
// Input lines:
//   set <x> <y> <z> <block>               (sets a block, default state)
//   mod <seed> <x> <y> <z> <json>         (prints emitted positions)
// Output for mod lines: the line, " => ", positions, " | " next long.
public final class Place {
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
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                var ops = RegistryOps.create(JsonOps.INSTANCE, level.registryAccess());
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ", 6);
                    if (a[0].equals("set")) {
                        BlockPos p = new BlockPos(Integer.parseInt(a[1]), Integer.parseInt(a[2]), Integer.parseInt(a[3]));
                        level.getChunk(p.getX() >> 4, p.getZ() >> 4);
                        level.setBlock(p, BuiltInRegistries.BLOCK.getValue(Identifier.parse(a[4])).defaultBlockState(), 2 | 16);
                        continue;
                    }
                    long seed = Long.parseLong(a[1]);
                    BlockPos pos = new BlockPos(Integer.parseInt(a[2]), Integer.parseInt(a[3]), Integer.parseInt(a[4]));
                    for (int cx = -1; cx <= 1; cx++) for (int cz = -1; cz <= 1; cz++) level.getChunk((pos.getX() >> 4) + cx, (pos.getZ() >> 4) + cz);
                    var r = new WorldgenRandom(new XoroshiroRandomSource(seed));
                    var m = PlacementModifier.CODEC.parse(ops, JsonParser.parseString(a[5])).getOrThrow();
                    var ctx = new PlacementContext(level, level.getChunkSource().getGenerator(), Optional.empty());
                    List<String> res = new ArrayList<>();
                    m.modify(ctx, r, pos, p -> res.add(p.getX() + "," + p.getY() + "," + p.getZ()));
                    out.println(line + " => " + String.join(" ", res) + " | " + r.nextLong());
                }
                f.complete(null);
            } catch (Throwable t) {
                f.completeExceptionally(t);
            }
        });
        try {
            f.get();
        } finally {
            out.flush();
            Runtime.getRuntime().halt(0);
        }
    }
}
