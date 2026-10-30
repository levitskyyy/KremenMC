package kremenmc.probe.jigsaw;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
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
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.structure.pools.alias.PoolAliasLookup;
import net.minecraft.world.level.levelgen.structure.structures.JigsawStructure;
import net.minecraft.world.level.storage.LevelStorageSource;

// AliasProbe resolves a jigsaw structure's pool aliases as a black box.
//   trace <structure> <seed>               forEachResolved with a logging legacy random seeded <seed>
//   lookup <structure> x y z seed ...      PoolAliasLookup.create(aliases, pos, seed): every alias -> target
public final class AliasProbe {
    static PrintStream OUT;

    static final class LogRandom extends LegacyRandomSource {
        LogRandom(long s) { super(s); }
        public int next(int bits) { int v = super.next(bits); OUT.println("  next" + bits + "=" + v); return v; }
        public int nextInt(int b) { int v = super.nextInt(b); OUT.println("  nextInt(" + b + ")=" + v); return v; }
    }

    public static void main(String[] args) throws Exception {
        OUT = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "aliasworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var ra = server.overworld().registryAccess();
                var st = (JigsawStructure) ra.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.parse(args[1]))).value();
                var al = st.getPoolAliases();
                var keys = new java.util.ArrayList<ResourceKey<net.minecraft.world.level.levelgen.structure.pools.StructureTemplatePool>>();
                al.forEach(b -> b.forEachResolved(new LegacyRandomSource(0), (k, v) -> { if (!keys.contains(k)) keys.add(k); }));
                keys.sort((a, b) -> a.identifier().toString().compareTo(b.identifier().toString()));
                if (args[0].equals("trace")) {
                    var r = new LogRandom(Long.parseLong(args[2]));
                    al.forEach(b -> { OUT.println("binding " + b.getClass().getSimpleName()); b.forEachResolved(r, (k, v) -> OUT.println("  " + k.identifier() + " -> " + v.identifier())); });
                } else {
                    for (int i = 2; i + 3 < args.length; i += 4) {
                        var pos = new BlockPos(Integer.parseInt(args[i]), Integer.parseInt(args[i + 1]), Integer.parseInt(args[i + 2]));
                        long seed = Long.parseLong(args[i + 3]);
                        var lk = PoolAliasLookup.create(al, pos, seed);
                        StringBuilder b = new StringBuilder("L " + args[i] + " " + args[i + 1] + " " + args[i + 2] + " " + seed);
                        for (var k : keys) b.append(' ').append(lk.lookup(k).identifier().getPath().replaceAll(".*/", ""));
                        OUT.println(b);
                    }
                }
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try { f.get(); } finally { OUT.flush(); Runtime.getRuntime().halt(0); }
    }
}
