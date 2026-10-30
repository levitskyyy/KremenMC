package kremenmc.probe.jigsaw;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.structure.templatesystem.CappedProcessor;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructurePlaceSettings;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplate.StructureBlockInfo;
import net.minecraft.world.level.storage.LevelStorageSource;

// CapProbe runs a processor list's capped processors' finalizeProcessing
// (black box) on a synthetic block list: n blocks at (ox+i%16, oy+i/256, oz+(i/16)%16),
// every k-th one gravel, the rest stone. Prints the indices the capped
// processors changed, with the new block and NBT.
//   <processor_list> <ox> <oy> <oz> <n> <k> [log]   (log: settings random = logging legacy random seeded 0)
public final class CapProbe {
    static PrintStream OUT;

    static final class LogRandom extends LegacyRandomSource {
        LogRandom(long s) { super(s); }
        public int next(int bits) {
            int v = super.next(bits);
            var tr = Thread.currentThread().getStackTrace();
            StringBuilder b = new StringBuilder("  next" + bits + "=" + v);
            for (int k = 2; k < Math.min(tr.length, 7); k++) b.append(" @").append(tr[k].getClassName().replaceAll(".*\\.", "")).append('.').append(tr[k].getMethodName());
            OUT.println(b);
            return v;
        }
    }

    public static void main(String[] args) throws Exception {
        OUT = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "capworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        // Load the chunk the blocks sit in from this (non-server) thread, so
        // the processors' block reads do not load chunks on the server thread.
        int cx0 = Integer.parseInt(args[1]) >> 4, cz0 = Integer.parseInt(args[3]) >> 4;
        for (int dx = 0; dx <= 1; dx++) for (int dz = 0; dz <= 1; dz++) server.overworld().getChunk(cx0 + dx, cz0 + dz);
        OUT.println("loaded");
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var ra = server.overworld().registryAccess();
                // A level whose every block is air (no chunk loading).
                var level = (net.minecraft.world.level.ServerLevelAccessor) java.lang.reflect.Proxy.newProxyInstance(CapProbe.class.getClassLoader(),
                    new Class<?>[]{net.minecraft.world.level.ServerLevelAccessor.class}, (proxy, m, a) -> {
                        switch (m.getName()) {
                            case "getBlockState": return Blocks.AIR.defaultBlockState();
                            case "getFluidState": return net.minecraft.world.level.material.Fluids.EMPTY.defaultFluidState();
                            case "registryAccess": return ra;
                            case "getLevel": return server.overworld();
                            case "getSeed": return server.overworld().getSeed();
                            case "hashCode": return 0;
                            case "toString": return "airlevel";
                        }
                        if (m.isDefault()) return java.lang.reflect.InvocationHandler.invokeDefault(proxy, m, a);
                        throw new UnsupportedOperationException(m.toString());
                    });
                var pl = ra.lookupOrThrow(Registries.PROCESSOR_LIST).getOrThrow(ResourceKey.create(Registries.PROCESSOR_LIST, Identifier.parse(args[0]))).value();
                OUT.println("seed " + server.overworld().getSeed());
                int n = Integer.parseInt(args[4]), k = Integer.parseInt(args[5]);
                boolean log = args.length > 6 && args[6].equals("log");
                for (int oi = 1; oi + 2 < args.length; oi = (oi == 1 ? 7 : oi + 3)) {
                if (oi >= 7 && args.length <= 7) break;
                int ox = Integer.parseInt(args[oi]), oy = Integer.parseInt(args[oi + 1]), oz = Integer.parseInt(args[oi + 2]);
                OUT.println("origin " + ox + " " + oy + " " + oz);
                BlockPos origin = new BlockPos(ox, oy, oz);
                List<StructureBlockInfo> blocks = new ArrayList<>();
                for (int i = 0; i < n; i++) {
                    var pos = origin.offset(i % 16, i / 256, (i / 16) % 16);
                    blocks.add(new StructureBlockInfo(pos, (i % k == 0 ? Blocks.GRAVEL : Blocks.STONE).defaultBlockState(), null));
                }
                var settings = new StructurePlaceSettings();
                if (log) settings.setRandom(new LogRandom(0));
                List<StructureBlockInfo> cur = new ArrayList<>(blocks);
                for (var p : pl.list()) {
                    if (!(p instanceof CappedProcessor)) continue;
                    OUT.println("capped " + p);
                    cur = p.finalizeProcessing(level, origin, origin, blocks, cur, settings);
                }
                for (int i = 0; i < cur.size(); i++) {
                    var b = cur.get(i);
                    if (b.state() != blocks.get(i).state()) OUT.println("C " + i + " " + b.state() + " " + b.nbt());
                }
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace(OUT);
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { OUT.flush(); Runtime.getRuntime().halt(0); }
    }
}
