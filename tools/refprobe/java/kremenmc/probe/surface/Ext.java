package kremenmc.probe.surface;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.lang.reflect.Method;
import java.util.Random;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.chunk.BlockColumn;
import net.minecraft.world.level.levelgen.material.MaterialSystem;

// Ext calls the private surface extensions of MaterialSystem on synthetic
// columns (stone up to s, water up to sea level - 1, air above) and logs
// every block read and write the extension makes.
// Args: <settings> <dim> <seed> <columns> <pointSeed>
public final class Ext {
    public static void main(String[] args) throws Exception {
        Env.boot();
        PrintStream o = new PrintStream(new BufferedOutputStream(Env.out, 1 << 16), false);
        try {
            Gen g = new Gen(args[0], args[1], Long.parseLong(args[2]));
            int n = Integer.parseInt(args[3]);
            Random r = new Random(Long.parseLong(args[4]));
            MaterialSystem ms = g.rs.surfaceSystem();
            Method eroded = Env.method(MaterialSystem.class, "erodedBadlandsExtension");
            Method frozen = Env.method(MaterialSystem.class, "frozenOceanExtension");
            var biomes = Env.server.registryAccess().lookupOrThrow(Registries.BIOME);
            Biome fo = biomes.getOrThrow(ResourceKey.create(Registries.BIOME, Identifier.parse("minecraft:frozen_ocean"))).value();
            Biome dfo = biomes.getOrThrow(ResourceKey.create(Registries.BIOME, Identifier.parse("minecraft:deep_frozen_ocean"))).value();
            LevelHeightAccessor lh = LevelHeightAccessor.create(g.minY(), g.height());
            int sea = g.settings.value().seaLevel();
            for (int i = 0; i < n; i++) {
                int x = r.nextInt(i % 2 == 0 ? 4000 : 60_000_000) - (i % 2 == 0 ? 2000 : 30_000_000);
                int z = r.nextInt(4000) - 2000;
                int s = 30 + r.nextInt(70);
                int h = r.nextInt(3) == 0 ? s + r.nextInt(5) - 2 : Math.max(s, sea - 1);
                int msl = s - r.nextInt(20);
                int kind = r.nextInt(3);
                BlockState[] col = new BlockState[g.height()];
                for (int y = 0; y < col.length; y++) {
                    int wy = y + g.minY();
                    col[y] = wy <= s ? Blocks.STONE.defaultBlockState() : wy < sea ? Blocks.WATER.defaultBlockState() : Blocks.AIR.defaultBlockState();
                }
                StringBuilder sb = new StringBuilder();
                BlockColumn bc = new BlockColumn() {
                    public BlockState getBlock(int y) {
                        sb.append(" g").append(y);
                        int i = y - g.minY();
                        return i < 0 || i >= col.length ? Blocks.AIR.defaultBlockState() : col[i];
                    }
                    public void setBlock(int y, BlockState st) {
                        sb.append(" s").append(y).append('=').append(Trace.stateString(st));
                        int i = y - g.minY();
                        if (i >= 0 && i < col.length) col[i] = st;
                    }
                };
                if (kind == 0) {
                    eroded.invoke(ms, bc, x, z, h, lh);
                    o.println("eroded " + x + " " + z + " " + s + " " + h + sb);
                } else {
                    Biome b = kind == 1 ? fo : dfo;
                    frozen.invoke(ms, msl, b, bc, new BlockPos.MutableBlockPos(), x, z, h);
                    o.println((kind == 1 ? "frozen" : "deepfrozen") + " " + x + " " + z + " " + s + " " + h + " " + msl + sb);
                }
            }
        } catch (Throwable t) {
            t.printStackTrace(o);
        }
        o.flush();
        Env.exit();
    }
}
