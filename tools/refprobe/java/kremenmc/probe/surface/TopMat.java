package kremenmc.probe.surface;

import java.io.PrintStream;
import java.util.List;
import java.util.Optional;
import net.minecraft.core.BlockPos;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.WorldGenerationContext;

// TopMat calls MaterialSystem.topMaterial (used by carvers to re-top
// uncovered dirt) on a generated chunk and logs the context the rule sees.
// Args: <settings> <dim> <seed> <cx> <cz>
public final class TopMat {
    public static void main(String[] args) throws Exception {
        Env.boot();
        PrintStream o = Env.out;
        try {
            Gen g = new Gen(args[0], args[1], Long.parseLong(args[2]));
            int cx = Integer.parseInt(args[3]), cz = Integer.parseInt(args[4]);
            ProtoChunk pc = g.newChunk(cx, cz);
            NoiseChunk nc = g.noiseChunk(pc);
            g.fill(nc, pc);
            g.surface(nc, pc, g.settings.value().materialRule().value(), g.src.possibleBiomes());
            var bm = g.biomeManager();
            var rule = new Trace.Rec(g.settings.value().materialRule().value(), o);
            for (int i = 0; i < 40; i++) {
                int x = cx * 16 + (i * 7) % 16, z = cz * 16 + (i * 5) % 16;
                int top = pc.getHeight(Heightmap.Types.WORLD_SURFACE_WG, x & 15, z & 15);
                int y = top - (i % 4);
                for (boolean f : new boolean[] {false, true}) {
                    o.print("call " + x + " " + y + " " + z + " " + f + " block " + Trace.stateString(pc.getBlockState(new BlockPos(x, y, z))) + " | ");
                    Optional<BlockState> r = g.rs.surfaceSystem().topMaterial(rule, g.rs, new WorldGenerationContext(g.gen, pc), bm::getBiome, pc, nc.cachingSamplers(), new BlockPos(x, y, z), f);
                    o.println("=> " + r.map(Trace::stateString).orElse("none"));
                }
            }
        } catch (Throwable t) {
            t.printStackTrace(o);
        }
        Env.exit();
    }
}
