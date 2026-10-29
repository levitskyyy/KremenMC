package kremenmc.probe.feat_disk;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;

// Bsp samples a registered block state provider with a seeded xoroshiro
// WorldgenRandom and logs every draw (LogRandom). Input lines:
//   <seed> <x> <y> <z> <block_state_provider key>
// Output: "R ..." draw lines, then the input line, " => ", the state and
// " | " the random's next long.
public final class Bsp {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out; LogRandom.OUT = out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        var bsps = VanillaRegistries.createWorldLookup().lookupOrThrow(Registries.BLOCK_STATE_PROVIDER);
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            if (line.isBlank()) continue;
            String[] f = line.split(" ", 5);
            var r = new LogRandom(new WorldgenRandom(new XoroshiroRandomSource(Long.parseLong(f[0]))));
            BlockPos pos = new BlockPos(Integer.parseInt(f[1]), Integer.parseInt(f[2]), Integer.parseInt(f[3]));
            var p = bsps.getOrThrow(ResourceKey.create(Registries.BLOCK_STATE_PROVIDER, Identifier.parse(f[4]))).value();
            String st = String.valueOf(p.getState(null, r, pos));
            r.on = false;
            out.println(line + " => " + st + " | " + r.nextLong());
        }
        out.flush();
        Runtime.getRuntime().halt(0);
    }
}
