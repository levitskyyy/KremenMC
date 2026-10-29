package kremenmc.probe.structlegacy;

import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.nbt.TagParser;
import net.minecraft.resources.RegistryOps;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.levelgen.structure.TemplateStructurePiece;
import net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext;
import net.minecraft.world.level.levelgen.structure.structures.RuinedPortalPiece;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructurePlaceSettings;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureProcessorType;
import net.minecraft.world.level.storage.LevelStorageSource;

// PortalSettings rebuilds ruined portal pieces from their saved NBT (one
// SNBT per line of the input file) and prints, by reflection on runtime
// state, the place settings: processors encoded with the processor codec,
// liquid settings, and getRandom(pos).nextLong() for a few positions.
public final class PortalSettings {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "structstartsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var level = server.overworld();
                var sctx = StructurePieceSerializationContext.fromLevel(level);
                var ops = RegistryOps.create(JsonOps.INSTANCE, level.registryAccess());
                var fld = TemplateStructurePiece.class.getDeclaredField("placeSettings");
                fld.setAccessible(true);
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    var tag = TagParser.parseCompoundFully(line);
                    var piece = new RuinedPortalPiece(sctx, tag);
                    StructurePlaceSettings ps = (StructurePlaceSettings) fld.get(piece);
                    StringBuilder sb = new StringBuilder("piece ").append(tag.getStringOr("Template", "?"));
                    for (var p : ps.getProcessors())
                        sb.append("\n  proc ").append(StructureProcessorType.SINGLE_CODEC.encodeStart(ops, p).result().map(Object::toString).orElse(p.getClass().getSimpleName()));
                    for (var fl : StructurePlaceSettings.class.getDeclaredFields()) {
                        if (java.lang.reflect.Modifier.isStatic(fl.getModifiers())) continue;
                        fl.setAccessible(true);
                        Object v = fl.get(ps);
                        if (v instanceof java.util.List) continue;
                        sb.append("\n  field ").append(fl.getName()).append('=').append(v);
                    }
                    if (args.length > 1) {
                        var bb = piece.getBoundingBox();
                        for (int cx = (bb.minX() - 8) >> 4; cx <= (bb.maxX() + 8) >> 4; cx++) for (int cz = (bb.minZ() - 8) >> 4; cz <= (bb.maxZ() + 8) >> 4; cz++) level.getChunk(cx, cz);
                        var r = new Starts.LogWG();
                        r.setSeed(Long.parseLong(args[1]));
                        Starts.log = new StringBuilder();
                        var clip = new net.minecraft.world.level.levelgen.structure.BoundingBox(bb.minX() - 8, -64, bb.minZ() - 8, bb.maxX() + 8, 319, bb.maxZ() + 8);
                        var before = new java.util.HashMap<BlockPos, net.minecraft.world.level.block.state.BlockState>();
                        for (int x = bb.minX() - 8; x <= bb.maxX() + 8; x++) for (int z = bb.minZ() - 8; z <= bb.maxZ() + 8; z++) for (int y = bb.minY() - 8; y <= bb.maxY() + 4; y++) before.put(new BlockPos(x, y, z), level.getBlockState(new BlockPos(x, y, z)));
                        piece.postProcess(level, level.structureManager(), level.getChunkSource().getGenerator(), r, clip, new net.minecraft.world.level.ChunkPos(bb.minX() >> 4, bb.minZ() >> 4), new BlockPos(bb.minX(), bb.minY(), bb.minZ()));
                        sb.append("\n  settingsRandomAfter=").append(ps.getRandom(BlockPos.ZERO) == r);
                        sb.append("\n  draws").append(Starts.log);
                        Starts.log = null;
                        sb.append("\n  writes");
                        for (var e : before.entrySet()) { var s = level.getBlockState(e.getKey()); if (s != e.getValue()) sb.append(" ").append(e.getKey().getX()).append(",").append(e.getKey().getY()).append(",").append(e.getKey().getZ()).append("=").append(s.toString().replace("Block{minecraft:", "").replace("}", "")); }
                    }
                    int[][] pts = {{0, 0, 0}, {1, 2, 3}, {1612, 12, -4383}, {-5, 70, 9}};
                    for (int[] q : pts) {
                        var r = ps.getRandom(new BlockPos(q[0], q[1], q[2]));
                        sb.append("\n  rand ").append(q[0]).append(',').append(q[1]).append(',').append(q[2]).append(' ').append(r.nextLong()).append(' ').append(r.nextLong());
                    }
                    out.println(sb);
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
