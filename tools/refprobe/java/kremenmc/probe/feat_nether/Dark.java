package kremenmc.probe.feat_nether;

import java.io.PrintStream;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Proxy;
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
import net.minecraft.world.level.LevelReader;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.storage.LevelStorageSource;

// Dark prints, for each block argument and each forced brightness (every
// brightness query of the reader answers it), the blocks (default states)
// it survives on: BlockState.canSurvive through a LevelReader proxy.
public final class Dark {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "featnetherworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                BlockPos pos = new BlockPos(0, 200, 0), below = pos.below();
                for (int light : new int[]{0, 12, 13, 15}) {
                    final int lv = light;
                    LevelReader r = (LevelReader) Proxy.newProxyInstance(Dark.class.getClassLoader(), new Class<?>[]{LevelReader.class}, (p, m, a) -> {
                        String n = m.getName();
                        if (n.equals("getRawBrightness") || n.equals("getMaxLocalRawBrightness")) return lv;
                        if (n.equals("getBrightness")) { out.println("QUERY getBrightness"); return lv; }
                        try { return m.invoke(level, a); } catch (InvocationTargetException e) { throw e.getCause(); }
                    });
                    for (String id : args) {
                        BlockState plant = BuiltInRegistries.BLOCK.getValue(Identifier.parse(id)).defaultBlockState();
                        StringBuilder sb = new StringBuilder("light=" + lv + " " + id);
                        for (Block b : BuiltInRegistries.BLOCK) {
                            level.setBlock(below, b.defaultBlockState(), 2 | 16);
                            if (plant.canSurvive(r, pos)) sb.append(' ').append(BuiltInRegistries.BLOCK.getKey(b).getPath());
                        }
                        out.println(sb);
                    }
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { out.flush(); Runtime.getRuntime().halt(0); }
    }
}
