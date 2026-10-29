package kremenmc.probe.jigsaw;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.storage.LevelStorageSource;

// PilePlace places a configured feature by key at (0,Y,0) on a flat
// stone floor (top at Y-1) with a logging xoroshiro WorldgenRandom, and
// prints every draw and every changed block in a 25x25x12 box. Input
// lines: <feature> <seed> <Y> [<dx,dy,dz=block> ...] (extra setup blocks,
// relative to (0,Y,0)).
public final class PilePlace {
    static PrintStream OUT;

    static final class LogR extends WorldgenRandom {
        int depth;
        LogR(long seed) { super(new XoroshiroRandomSource(seed)); }
        static final StackWalker W = StackWalker.getInstance();
        static final boolean PATH = System.getenv("PILEPATH") != null;
        static String path() {
            return W.walk(st -> st.filter(f -> f.getClassName().startsWith("net.minecraft"))
                .map(f -> { String c = f.getClassName(); c = c.substring(c.lastIndexOf('.') + 1); return c + "." + f.getMethodName() + ":" + f.getLineNumber(); })
                .filter(x -> !x.startsWith("WorldgenRandom") && !x.startsWith("Xoroshiro") && !x.startsWith("RandomSource"))
                .limit(3).collect(java.util.stream.Collectors.joining("<")));
        }
        <T> T log(String w, T v) { if (depth == 1) OUT.print(" " + w + "=" + v + (PATH ? "@" + path() : "")); return v; }
        public int nextInt(int b) { depth++; try { return log("i" + b, super.nextInt(b)); } finally { depth--; } }
        public int nextInt() { depth++; try { return log("i", super.nextInt()); } finally { depth--; } }
        public long nextLong() { depth++; try { return log("l", super.nextLong()); } finally { depth--; } }
        public boolean nextBoolean() { depth++; try { return log("b", super.nextBoolean()); } finally { depth--; } }
        public float nextFloat() { depth++; try { return log("f", super.nextFloat()); } finally { depth--; } }
        public double nextDouble() { depth++; try { return log("d", super.nextDouble()); } finally { depth--; } }
        public int nextIntBetweenInclusive(int a, int b) { depth++; try { return log("r" + a + "_" + b, super.nextIntBetweenInclusive(a, b)); } finally { depth--; } }
    }

    static String state(BlockState s) {
        StringBuilder b = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath());
        String sep = "[";
        for (Property<?> p : s.getProperties()) {
            b.append(sep).append(p.getName()).append('=').append(s.getValue(p).toString().toLowerCase());
            sep = ",";
        }
        if (!sep.equals("[")) b.append(']');
        return b.toString();
    }

    public static void main(String[] args) throws Exception {
        OUT = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "pileworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                var ops = net.minecraft.resources.RegistryOps.create(com.mojang.serialization.JsonOps.INSTANCE, level.registryAccess());
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ");
                    long seed = Long.parseLong(a[1]);
                    int y0 = Integer.parseInt(a[2]);
                    for (int cx = -1; cx <= 0; cx++)
                        for (int cz = -1; cz <= 0; cz++) level.getChunk(cx, cz);
                    BlockState[][][] before = new BlockState[25][12][25];
                    for (int dx = -12; dx <= 12; dx++)
                        for (int dz = -12; dz <= 12; dz++)
                            for (int dy = -4; dy < 8; dy++) {
                                BlockState s = dy < 0 ? Blocks.STONE.defaultBlockState() : Blocks.AIR.defaultBlockState();
                                level.setBlock(new BlockPos(dx, y0 + dy, dz), s, 2 | 16);
                            }
                    for (int k = 3; k < a.length; k++) {
                        String[] kv = a[k].split("=", 2);
                        String[] c = kv[0].split(",");
                        var st = net.minecraft.commands.arguments.blocks.BlockStateParser.parseForBlock(BuiltInRegistries.BLOCK, kv[1], false).blockState();
                        level.setBlock(new BlockPos(Integer.parseInt(c[0]), y0 + Integer.parseInt(c[1]), Integer.parseInt(c[2])), st, 2 | 16);
                    }
                    for (int dx = -12; dx <= 12; dx++)
                        for (int dz = -12; dz <= 12; dz++)
                            for (int dy = -4; dy < 8; dy++) before[dx + 12][dy + 4][dz + 12] = level.getBlockState(new BlockPos(dx, y0 + dy, dz));
                    var cf = net.minecraft.world.level.levelgen.feature.Feature.CODEC.parse(ops, new com.google.gson.JsonPrimitive(a[0])).getOrThrow().value();
                    OUT.print(line + " :");
                    var r = new LogR(seed);
                    boolean ok = cf.place(level, level.getChunkSource().getGenerator(), r, new BlockPos(0, y0, 0));
                    OUT.print(" => " + ok);
                    for (int dy = -4; dy < 8; dy++)
                        for (int dx = -12; dx <= 12; dx++)
                            for (int dz = -12; dz <= 12; dz++) {
                                BlockState s = level.getBlockState(new BlockPos(dx, y0 + dy, dz));
                                if (s != before[dx + 12][dy + 4][dz + 12]) OUT.print(" " + dx + "," + dy + "," + dz + "=" + state(s));
                            }
                    OUT.println();
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
            OUT.flush();
            Runtime.getRuntime().halt(0);
        }
    }
}
