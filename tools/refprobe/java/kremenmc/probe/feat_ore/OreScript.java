package kremenmc.probe.feat_ore;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.RegistryOps;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.feature.Feature;
import net.minecraft.world.level.storage.LevelStorageSource;

// OreScript places a feature in the OrePlace scene with scripted random
// values, to observe the shape as a function of each draw. Input lines:
//   <x> <y> <z> <v1,v2,...> <json>
// Output: the line, tab, placed result, changed blocks, " | calls=N".
public final class OreScript {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featoreworld-");
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
                int R = OrePlace.R;
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ", 5);
                    int x = Integer.parseInt(a[0]), y = Integer.parseInt(a[1]), z = Integer.parseInt(a[2]);
                    for (int cx = (x - R - 16) >> 4; cx <= (x + R + 16) >> 4; cx++)
                        for (int cz = (z - R - 16) >> 4; cz <= (z + R + 16) >> 4; cz++) level.getChunk(cx, cz);
                    for (int dx = -R; dx <= R; dx++)
                        for (int dz = -R; dz <= R; dz++)
                            for (int yy = OrePlace.Y0; yy <= OrePlace.Y1; yy++)
                                level.setBlock(new BlockPos(x + dx, yy, z + dz), OrePlace.setup(dx, yy, dz), 2 | 16);
                    var js = JsonParser.parseString(a[4]);
                    Feature cf = js.isJsonPrimitive() ? Feature.CODEC.parse(ops, js).getOrThrow().value() : Feature.DIRECT_CODEC.parse(ops, js).getOrThrow();
                    OreScriptRandom r = new OreScriptRandom();
                    for (String v : a[3].split(",")) r.q.add(Double.parseDouble(v));
                    StringBuilder sb = new StringBuilder(line).append("\t");
                    try {
                        sb.append(cf.place(level, level.getChunkSource().getGenerator(), r, new BlockPos(x, y, z)));
                    } catch (Throwable t) {
                        sb.append("ERR ").append(t.getMessage());
                    }
                    for (int yy = OrePlace.Y0; yy <= OrePlace.Y1; yy++)
                        for (int dx = -R; dx <= R; dx++)
                            for (int dz = -R; dz <= R; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(x + dx, yy, z + dz));
                                if (s != OrePlace.setup(dx, yy, dz)) sb.append(' ').append(dx).append(',').append(yy).append(',').append(dz);
                            }
                    out.println(sb.append(" | calls=").append(r.calls).append(" left=").append(r.q.size()));
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
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
