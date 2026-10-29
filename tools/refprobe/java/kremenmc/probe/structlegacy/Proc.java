package kremenmc.probe.structlegacy;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.commands.arguments.blocks.BlockStateParser;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.resources.RegistryOps;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructurePlaceSettings;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureProcessorType;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplate;
import net.minecraft.world.level.storage.LevelStorageSource;

// Proc runs one structure processor (JSON, processor codec) as a black box
// on a block at positions (x, y0, z0) for x in [0, n) and prints the
// positions whose output differs from the input, with the output state.
// Input lines: <state> <y0> <z0> <n> <offx,offy,offz> <json>
public final class Proc {
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
                var ops = RegistryOps.create(JsonOps.INSTANCE, level.registryAccess());
                for (String line : lines) {
                    if (line.isBlank()) continue;
                    String[] a = line.split(" ", 6);
                    var st = BlockStateParser.parseForBlock(level.registryAccess().lookupOrThrow(Registries.BLOCK), a[0], false).blockState();
                    int y0 = Integer.parseInt(a[1]), z0 = Integer.parseInt(a[2]), n = Integer.parseInt(a[3]);
                    String[] o = a[4].split(",");
                    BlockPos off = new BlockPos(Integer.parseInt(o[0]), Integer.parseInt(o[1]), Integer.parseInt(o[2]));
                    var p = StructureProcessorType.SINGLE_CODEC.parse(ops, JsonParser.parseString(a[5])).getOrThrow();
                    var ps = new StructurePlaceSettings();
                    StringBuilder sb = new StringBuilder(a[0]).append(' ').append(a[4]).append(" =>");
                    for (int x = 0; x < n; x++) {
                        BlockPos pos = new BlockPos(x, y0, z0);
                        var info = new StructureTemplate.StructureBlockInfo(pos, st, null);
                        Starts.LogWG lr = null;
                        if (args.length > 1) {
                            lr = new Starts.LogWG();
                            lr.setSeed(net.minecraft.util.Mth.getSeed(pos));
                            ps.setRandom(lr);
                            Starts.log = new StringBuilder();
                        }
                        var res = p.processBlock(level, off, BlockPos.ZERO, pos, info, ps);
                        if (res == null) sb.append(' ').append(x).append("=null");
                        else if (res.state() != st) sb.append(' ').append(x).append('=').append(res.state().toString().replace("Block{minecraft:", "").replace("}", ""));
                        if (lr != null) { sb.append(" [").append(x).append(Starts.log).append("]"); Starts.log = null; }
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
