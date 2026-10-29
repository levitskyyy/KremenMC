package kremenmc.probe.legacy;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.TagParser;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.StructurePiece;
import net.minecraft.world.level.levelgen.structure.structures.MineshaftPieces;
import net.minecraft.world.level.storage.LevelStorageSource;
import net.minecraft.world.phys.AABB;

// MinePlace runs a mineshaft piece's postProcess as a black box on the flat
// GameTest world. Input lines: <scene> <seed> <snbt piece compound>.
// scene: stone (the region x,z -24..24, y -59..-20 is stone) or air.
// The clip box is the whole region. Prints the draws of the xoroshiro
// WorldgenRandom (LOG=1), every block of the region that differs from the
// scene, block entities and entities in the region, and the next long.
public final class MinePlace {
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

    static BlockState scene(String s) { return s.equals("stone") ? Blocks.STONE.defaultBlockState() : Blocks.AIR.defaultBlockState(); }

    public static void main(String[] args) throws Exception {
        PrintStream out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        LogWR.OUT = out;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "mineplaceworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ", 3);
                    long seed = Long.parseLong(a[1]);
                    CompoundTag tag = TagParser.parseCompoundFully(a[2]);
                    for (int cx = -2; cx <= 1; cx++) for (int cz = -2; cz <= 1; cz++) level.getChunk(cx, cz);
                    for (var e : level.getEntities((net.minecraft.world.entity.Entity) null, new AABB(-30, -70, -30, 30, 0, 30), e -> true)) e.discard();
                    for (int x = -24; x <= 24; x++) for (int z = -24; z <= 24; z++) for (int y = -59; y <= -20; y++) {
                        BlockPos p = new BlockPos(x, y, z);
                        level.removeBlockEntity(p);
                        level.setBlock(p, scene(a[0]), 2 | 16);
                    }
                    StructurePiece piece = switch (tag.getStringOr("id", "")) {
                        case "minecraft:msroom" -> new MineshaftPieces.MineShaftRoom(tag);
                        case "minecraft:mscorridor" -> new MineshaftPieces.MineShaftCorridor(tag);
                        case "minecraft:mscrossing" -> new MineshaftPieces.MineShaftCrossing(tag);
                        default -> new MineshaftPieces.MineShaftStairs(tag);
                    };
                    LogWR r = new LogWR(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed));
                    r.on = System.getenv("LOG") != null;
                    out.println("CASE " + line);
                    piece.postProcess(level, level.structureManager(), level.getChunkSource().getGenerator(), r,
                        new BoundingBox(-24, -59, -24, 24, -20, 24), new ChunkPos(0, 0), BlockPos.ZERO);
                    r.on = false;
                    StringBuilder sb = new StringBuilder();
                    for (int y = -59; y <= -20; y++) for (int x = -24; x <= 24; x++) for (int z = -24; z <= 24; z++) {
                        BlockPos p = new BlockPos(x, y, z);
                        BlockState s = level.getBlockState(p);
                        if (s != scene(a[0])) sb.append("B ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(state(s)).append('\n');
                        var be = level.getBlockEntity(p);
                        if (be != null) sb.append("BE ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(be.saveWithoutMetadata(level.registryAccess())).append('\n');
                    }
                    for (var e : level.getEntities((net.minecraft.world.entity.Entity) null, new AABB(-30, -70, -30, 30, 0, 30), e -> true)) {
                        var to = net.minecraft.world.level.storage.TagValueOutput.createWithContext(net.minecraft.util.ProblemReporter.DISCARDING, level.registryAccess());
                        e.saveWithoutId(to);
                        CompoundTag et = to.buildResult();
                        sb.append("E ").append(BuiltInRegistries.ENTITY_TYPE.getKey(e.getType())).append(' ').append(e.getX()).append(' ').append(e.getY()).append(' ').append(e.getZ())
                            .append(' ').append(et.get("LootTable")).append(' ').append(et.get("LootTableSeed")).append(' ').append(et.get("Rotation")).append('\n');
                    }
                    out.print(sb);
                    out.println("NEXT " + r.nextLong());
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
