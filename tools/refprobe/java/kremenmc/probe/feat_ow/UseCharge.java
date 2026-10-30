package kremenmc.probe.feat_ow;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.SculkBehaviour;
import net.minecraft.world.level.block.SculkSpreader;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.storage.LevelStorageSource;

// UseCharge calls SculkBlock.attemptUseCharge on a lone cursor and prints the
// draws and the returned charge. Input lines:
//   <charge> <ox> <oy> <oz> <seed> <above> [sensors]
// The cursor sits on sculk at (0, 64, 0) inside stone; <above> is the block
// at (0, 65, 0); the origin is (ox, oy, oz) relative to the cursor; sensors
// puts that many sculk sensors at (2, 65, 0), (-2, 65, 0), ...
public final class UseCharge {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        LogRandom.OUT = out;
        System.setOut(System.err);
        var lines = Files.readAllLines(Path.of(args[0]));
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featowworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                for (int cx = -2; cx <= 2; cx++) for (int cz = -2; cz <= 2; cz++) level.getChunk(cx, cz);
                BlockPos c = new BlockPos(0, 64, 0);
                for (String line : lines) {
                    if (line.isBlank() || line.startsWith("#")) continue;
                    String[] a = line.trim().split(" +");
                    int charge = Integer.parseInt(a[0]);
                    BlockPos o = c.offset(Integer.parseInt(a[1]), Integer.parseInt(a[2]), Integer.parseInt(a[3]));
                    long seed = Long.parseLong(a[4]);
                    int sensors = a.length > 6 ? Integer.parseInt(a[6]) : 0;
                    for (int dx = -6; dx <= 6; dx++)
                        for (int dy = -6; dy <= 6; dy++)
                            for (int dz = -6; dz <= 6; dz++)
                                level.setBlock(c.offset(dx, dy, dz), dy > 0 ? Blocks.AIR.defaultBlockState() : Blocks.STONE.defaultBlockState(), 2 | 16);
                    level.setBlock(c, Blocks.SCULK.defaultBlockState(), 2 | 16);
                    BlockState above = switch (a[5]) {
                        case "stone" -> Blocks.STONE.defaultBlockState();
                        case "water" -> Blocks.WATER.defaultBlockState();
                        default -> Blocks.AIR.defaultBlockState();
                    };
                    level.setBlock(c.above(), above, 2 | 16);
                    for (int s = 0; s < sensors; s++)
                        level.setBlock(c.offset(s % 2 == 0 ? 2 + s / 2 : -2 - s / 2, 1, 0), Blocks.SCULK_SENSOR.defaultBlockState(), 2 | 16);
                    if (a.length > 7)
                        for (String t : a[7].split(";")) {
                            String[] q = t.split(",");
                            level.setBlock(c.offset(Integer.parseInt(q[0]), Integer.parseInt(q[1]), Integer.parseInt(q[2])), q.length > 3 ? Blocks.SCULK_SHRIEKER.defaultBlockState() : Blocks.SCULK_SENSOR.defaultBlockState(), 2 | 16);
                        }
                    var cursor = new SculkSpreader.ChargeCursor(c, charge);
                    var sp = SculkSpreader.createWorldGenSpreader();
                    var r = new LogRandom(new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed)));
                    r.on = true;
                    out.println("C " + line);
                    int got = ((SculkBehaviour) Blocks.SCULK).attemptUseCharge(cursor, level, o, r, sp, true);
                    r.on = false;
                    out.println(line + "\t" + got + " above=" + net.minecraft.core.registries.BuiltInRegistries.BLOCK.getKey(level.getBlockState(c.above()).getBlock()).getPath() + " | " + r.nextLong());
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
