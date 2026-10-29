package kremenmc.probe.structlegacy;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.Random;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.entity.RandomizableContainerBlockEntity;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.structures.BuriedTreasurePieces;
import net.minecraft.world.level.storage.LevelStorageSource;

// Buried builds random 3x3 columns (materials from a palette, seeded per
// case) around x=9,z=9 of a chunk of the flat test world, places a buried
// treasure piece at (x,90,z) with postProcess (legacy WorldgenRandom seeded
// with the case seed, chunk box as clip) and prints the case setup, every
// changed block, the piece box, the chest loot and the random's next long.
// Args: <cases> <first seed>.
public final class Buried {
    static BlockState[] PAL;
    static void initPal() { PAL = new BlockState[]{Blocks.AIR.defaultBlockState(), Blocks.WATER.defaultBlockState(), Blocks.SAND.defaultBlockState(),
        Blocks.SANDSTONE.defaultBlockState(), Blocks.STONE.defaultBlockState(), Blocks.GRAVEL.defaultBlockState(),
        Blocks.DIRT.defaultBlockState(), Blocks.RED_SAND.defaultBlockState(), Blocks.CLAY.defaultBlockState(), Blocks.DEEPSLATE.defaultBlockState(), Blocks.GRANITE.defaultBlockState(), Blocks.ANDESITE.defaultBlockState(), Blocks.DIORITE.defaultBlockState(), Blocks.TUFF.defaultBlockState(), Blocks.RED_SANDSTONE.defaultBlockState(), Blocks.TERRACOTTA.defaultBlockState(), Blocks.COBBLESTONE.defaultBlockState(), Blocks.SMOOTH_SANDSTONE.defaultBlockState()}; }

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
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "structlegacyworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        initPal();
        int cases = Integer.parseInt(args[0]);
        long first = Long.parseLong(args[1]);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                for (int c = 0; c < cases; c++) {
                    long seed = first + c;
                    Random cr = new Random(seed);
                    int cx = c * 2, cz = 0;
                    var chunk = new ChunkPos(cx, cz);
                    level.getChunk(cx, cz);
                    int x = cx * 16 + 9, z = cz * 16 + 9;
                    StringBuilder sb = new StringBuilder("case ").append(seed);
                    BlockState[][] setup = new BlockState[9][200];
                    for (int i = 0; i < 9; i++) {
                        int dx = i % 3 - 1, dz = i / 3 - 1;
                        int y = -63;
                        sb.append(" col").append(dx).append(',').append(dz).append(':');
                        while (y < 120) {
                            int len = 1 + cr.nextInt(cr.nextInt(4) == 0 ? 40 : 6);
                            int top = y > 75 ? 0 : (y > 55 && cr.nextBoolean() ? 1 : cr.nextInt(PAL.length));
                            BlockState s = PAL[top];
                            sb.append(y).append('=').append(state(s)).append(';');
                            for (int k = 0; k < len && y < 120; k++, y++) setup[i][y + 64] = s;
                        }
                        for (int yy = -63; yy < 120; yy++) level.setBlock(new BlockPos(x + dx, yy, z + dz), setup[i][yy + 64], 2 | 16);
                    }
                    var piece = new BuriedTreasurePieces.BuriedTreasurePiece(new BlockPos(x, 90, z));
                    var r = new WorldgenRandom(new LegacyRandomSource(seed));
                    BoundingBox clip = new BoundingBox(cx * 16, -64, cz * 16, cx * 16 + 15, 319, cz * 16 + 15);
                    piece.postProcess(level, level.structureManager(), level.getChunkSource().getGenerator(), r, clip, chunk, new BlockPos(x, 90, z));
                    sb.append(" =>");
                    for (int i = 0; i < 9; i++) {
                        int dx = i % 3 - 1, dz = i / 3 - 1;
                        for (int yy = -63; yy < 120; yy++) {
                            BlockPos p = new BlockPos(x + dx, yy, z + dz);
                            BlockState s = level.getBlockState(p);
                            if (s != setup[i][yy + 64]) sb.append(' ').append(dx).append(',').append(yy).append(',').append(dz).append('=').append(state(s));
                            if (level.getBlockEntity(p) instanceof RandomizableContainerBlockEntity be)
                                sb.append(" loot@").append(yy).append('=').append(be.getLootTable() == null ? "null" : be.getLootTable().identifier()).append('/').append(be.getLootTableSeed());
                        }
                    }
                    for (int dx = -2; dx <= 2; dx++) for (int dz = -2; dz <= 2; dz++) {
                        if (Math.abs(dx) < 2 && Math.abs(dz) < 2) continue;
                        for (int yy = -63; yy < 120; yy++) {
                            BlockPos p = new BlockPos(x + dx, yy, z + dz);
                            BlockState s = level.getBlockState(p);
                            if (false) sb.append(" OUT").append(dx).append(',').append(yy).append(',').append(dz).append('=').append(state(s));
                        }
                    }
                    sb.append(" box=").append(piece.getBoundingBox()).append(" next=").append(r.nextLong());
                    out.println(sb);
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
