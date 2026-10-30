package kremenmc.probe.temple;

import com.mojang.serialization.JsonOps;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.nbt.TagParser;
import net.minecraft.resources.RegistryOps;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.levelgen.structure.TemplateStructurePiece;
import net.minecraft.world.level.levelgen.structure.structures.OceanRuinPieces;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructurePlaceSettings;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureProcessorType;
import net.minecraft.world.level.storage.LevelStorageSource;

// OrSettings rebuilds ocean ruin pieces from saved SNBT (one per line) and
// prints, by reflection on runtime state, their place settings: the
// processors encoded with the processor codec (class name when not
// encodable) and the non-list fields.
public final class OrSettings {
    public static void main(String[] args) throws Exception {
        var out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "orsettingsworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                var level = server.overworld();
                var ops = RegistryOps.create(JsonOps.INSTANCE, level.registryAccess());
                var fld = TemplateStructurePiece.class.getDeclaredField("placeSettings");
                fld.setAccessible(true);
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    var piece = OceanRuinPieces.OceanRuinPiece.create(server.getStructureTemplateManager(), TagParser.parseCompoundFully(line));
                    StructurePlaceSettings ps = (StructurePlaceSettings) fld.get(piece);
                    StringBuilder sb = new StringBuilder("piece ").append(line);
                    for (var p : ps.getProcessors())
                        sb.append("\n  proc ").append(p.getClass().getSimpleName()).append(' ').append(StructureProcessorType.SINGLE_CODEC.encodeStart(ops, p).result().map(Object::toString).orElse("?"));
                    for (var fl : StructurePlaceSettings.class.getDeclaredFields()) {
                        if (java.lang.reflect.Modifier.isStatic(fl.getModifiers())) continue;
                        fl.setAccessible(true);
                        Object v = fl.get(ps);
                        if (v instanceof java.util.List) continue;
                        sb.append("\n  field ").append(fl.getName()).append('=').append(v);
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
