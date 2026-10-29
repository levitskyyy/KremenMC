package kremenmc.probe.surface;

import com.mojang.serialization.MapCodec;
import java.io.BufferedOutputStream;
import java.io.DataOutputStream;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.zip.GZIPOutputStream;
import net.minecraft.core.Holder;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.chunk.LevelChunkSection;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.material.MaterialRuleContext;
import net.minecraft.world.level.levelgen.material.rule.MaterialRule;
import net.minecraft.world.level.levelgen.material.rule.RuleEvaluator;

// Trace generates chunks like the TERRAIN status without carvers (see Gen)
// and writes, per chunk, the blocks after the noise fill and after the
// material pass (blocks.bin.gz) plus, for traced chunks, every material rule
// evaluation with the context values the rule can observe (trace-X-Z.txt.gz).
// Args: <settings> <dim> <seed> <outdir> <possible: chunk|all[-empty]> <cx,cz[,t]>...
// (possible biomes: the chunk's own or all; "-empty" runs an empty rule,
// "-both" also writes an "ext" record generated with an empty rule)
// (",t" marks a traced chunk).
public final class Trace {
    static String stateString(BlockState s) {
        StringBuilder sb = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString());
        var props = new TreeMap<String, String>();
        for (Property<?> p : s.getProperties()) props.put(p.getName(), name(p, s));
        if (!props.isEmpty()) {
            sb.append('[');
            boolean first = true;
            for (var e : props.entrySet()) {
                if (!first) sb.append(',');
                first = false;
                sb.append(e.getKey()).append('=').append(e.getValue());
            }
            sb.append(']');
        }
        return sb.toString();
    }

    static <T extends Comparable<T>> String name(Property<T> p, BlockState s) {
        return p.getName(s.getValue(p));
    }

    static void writeChunk(DataOutputStream o, String tag, ProtoChunk pc) throws Exception {
        o.writeUTF(tag);
        o.writeInt(pc.getPos().x());
        o.writeInt(pc.getPos().z());
        LevelChunkSection[] secs = pc.getSections();
        o.writeInt(secs.length);
        Map<BlockState, Integer> pal = new HashMap<>();
        List<BlockState> order = new ArrayList<>();
        int[] idx = new int[4096 * secs.length];
        for (int s = 0; s < secs.length; s++)
            for (int y = 0; y < 16; y++)
                for (int z = 0; z < 16; z++)
                    for (int x = 0; x < 16; x++) {
                        BlockState st = secs[s].getBlockState(x, y, z);
                        Integer i = pal.get(st);
                        if (i == null) {
                            i = order.size();
                            pal.put(st, i);
                            order.add(st);
                        }
                        idx[(s * 16 + y) * 256 + z * 16 + x] = i;
                    }
        o.writeInt(order.size());
        for (BlockState st : order) o.writeUTF(stateString(st));
        for (int v : idx) o.writeShort(v);
        for (Heightmap.Types t : new Heightmap.Types[] {Heightmap.Types.WORLD_SURFACE_WG, Heightmap.Types.OCEAN_FLOOR_WG}) {
            for (int z = 0; z < 16; z++)
                for (int x = 0; x < 16; x++) o.writeInt(pc.getHeight(t, x, z));
        }
    }

    record Rec(MaterialRule inner, PrintStream out) implements MaterialRule {
        @Override
        public RuleEvaluator compile(MaterialRuleContext c) {
            RuleEvaluator e = inner.compile(c);
            return (x, y, z) -> {
                BlockState r = e.tryApply(x, y, z);
                out.println(x + " " + y + " " + z + " " + c.stoneDepthAbove() + " " + c.stoneDepthBelow() + " " + c.surfaceDepth() + " " + c.waterHeight() + " "
                    + c.getMinSurfaceLevel() + " " + Double.doubleToRawLongBits(c.getSurfaceSecondary()) + " " + c.surfaceGradientX() + " " + c.surfaceGradientZ() + " "
                    + c.getBiome().getRegisteredName() + " " + (r == null ? "-" : stateString(r)));
                return r;
            };
        }

        @Override
        public MapCodec<? extends MaterialRule> codec() {
            return null;
        }
    }

    public static void main(String[] args) throws Exception {
        Env.boot();
        try {
            Gen g = new Gen(args[0], args[1], Long.parseLong(args[2]));
            Path dir = Path.of(args[3]);
            Files.createDirectories(dir);
            boolean all = args[4].startsWith("all"), empty = args[4].endsWith("-empty"), both = args[4].endsWith("-both");
            try (DataOutputStream o = new DataOutputStream(new BufferedOutputStream(new GZIPOutputStream(Files.newOutputStream(dir.resolve("blocks.bin.gz")), 1 << 16)))) {
                for (int i = 5; i < args.length; i++) {
                    String[] f = args[i].split(",");
                    int cx = Integer.parseInt(f[0]), cz = Integer.parseInt(f[1]);
                    boolean traced = f.length > 2;
                    ProtoChunk pc = g.newChunk(cx, cz);
                    NoiseChunk nc = g.noiseChunk(pc);
                    g.fill(nc, pc);
                    writeChunk(o, "noise", pc);
                    Set<Holder<Biome>> possible = all ? g.src.possibleBiomes() : g.chunkBiomes(pc);
                    MaterialRule rule = empty ? new net.minecraft.world.level.levelgen.material.rule.SequenceRule(List.of()) : g.settings.value().materialRule().value();
                    if (traced) {
                        try (PrintStream t = new PrintStream(new BufferedOutputStream(new GZIPOutputStream(Files.newOutputStream(dir.resolve("trace-" + cx + "-" + cz + ".txt.gz")), 1 << 16)), false)) {
                            g.surface(nc, pc, new Rec(rule, t), possible);
                        }
                    } else {
                        g.surface(nc, pc, rule, possible);
                    }
                    writeChunk(o, "surface", pc);
                    nc.close();
                    if (both) {
                        pc = g.newChunk(cx, cz);
                        nc = g.noiseChunk(pc);
                        g.fill(nc, pc);
                        g.surface(nc, pc, new net.minecraft.world.level.levelgen.material.rule.SequenceRule(List.of()), all ? g.src.possibleBiomes() : g.chunkBiomes(pc));
                        writeChunk(o, "ext", pc);
                        nc.close();
                    }
                }
            }
            Env.out.println("ok");
        } catch (Throwable t) {
            t.printStackTrace(Env.out);
        }
        Env.exit();
    }
}
