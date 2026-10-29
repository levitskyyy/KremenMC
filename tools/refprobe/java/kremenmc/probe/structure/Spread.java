package kremenmc.probe.structure;

import java.io.PrintStream;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.levelgen.structure.placement.RandomSpreadStructurePlacement;

// Spread samples every random_spread structure set of the vanilla registry
// as a black box. Args: <seed> <cell radius> <chunk radius>. For each set,
// sorted by key, and each grid cell gx,gz in [-R,R]: the potential chunk of
// the cell (getPotentialStructureChunk at the cell's first chunk), whether
// applyAdditionalChunkRestrictions accepts it, and getLocatePos. Sets with
// spacing 1 are sampled per chunk in [-C,C] instead.
public final class Spread {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        long seed = Long.parseLong(args[0]);
        int r = Integer.parseInt(args[1]);
        int cr = Integer.parseInt(args[2]);
        var lookup = VanillaRegistries.createWorldLookup();
        var sets = lookup.lookupOrThrow(Registries.STRUCTURE_SET);
        List<String> keys = new ArrayList<>();
        sets.listElementIds().forEach(k -> keys.add(k.identifier().toString()));
        keys.sort(null);
        for (String k : keys) {
            var set = sets.listElements().filter(h -> h.key().identifier().toString().equals(k)).findFirst().orElseThrow().value();
            if (!(set.placement() instanceof RandomSpreadStructurePlacement p)) continue;
            StringBuilder sb = new StringBuilder();
            if (p.spacing() == 1) {
                for (int cz = -cr; cz <= cr; cz++) for (int cx = -cr; cx <= cr; cx++) {
                    if (p.applyAdditionalChunkRestrictions(cx, cz, seed)) sb.append(k).append(' ').append(cx).append(' ').append(cz).append('\n');
                }
            } else {
                for (int gz = -r; gz <= r; gz++) for (int gx = -r; gx <= r; gx++) {
                    ChunkPos c = p.getPotentialStructureChunk(seed, gx * p.spacing(), gz * p.spacing());
                    var lp = p.getLocatePos(c);
                    sb.append(k).append(' ').append(gx).append(' ').append(gz).append(' ')
                        .append(c.x()).append(' ').append(c.z()).append(' ')
                        .append(p.applyAdditionalChunkRestrictions(c.x(), c.z(), seed) ? 1 : 0).append(' ')
                        .append(lp.getX()).append(' ').append(lp.getZ()).append('\n');
                }
            }
            out.print(sb);
        }
        out.flush();
    }
}
