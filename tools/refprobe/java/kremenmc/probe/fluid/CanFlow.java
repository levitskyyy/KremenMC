package kremenmc.probe.fluid;

import java.util.*;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;

// CanFlow observes, for every block's default state, what a water or lava
// source does to it from the side. Each block sits in a closed stone pocket
// next to the source (placed with neighbour updates), and is read back one
// tick later and after 45 ticks. Output per block and fluid:
//
//	<fluid> <block> <after1> <after45>
//
// where each column is "same", "fluid" (the cell holds the fluid's own
// block), or the observed state.
public final class CanFlow {
    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule advance_time false");
            Env.command("gamerule advance_weather false");
            Env.command("gamerule random_tick_speed 0");
            Env.command("gamerule max_block_modifications 10000000");
        });
        Env.waitTicks(5);
        List<Block> blocks = new ArrayList<>();
        for (Block b : BuiltInRegistries.BLOCK) {
            if (b.defaultBlockState().isAir() || b == Blocks.WATER || b == Blocks.LAVA) continue;
            blocks.add(b);
        }
        String[] fluids = {"water", "lava"};
        int per = 48;
        int cells = blocks.size() * 2;
        int rows = (cells + per - 1) / per;
        int xs = per * 4, zs = rows * 3;
        for (int cx = 0; cx <= xs; cx += 128)
            for (int cz = 0; cz <= zs; cz += 128) {
                int ax = cx, az = cz;
                Env.run(() -> Env.command(String.format("forceload add %d %d %d %d", 1000 + ax, 1000 + az, 1000 + Math.min(ax + 127, xs), 1000 + Math.min(az + 127, zs))));
            }
        Env.run(() -> {
            int k = 0;
            for (int x = 8; x < xs + 16; x += 24)
                for (int z = 8; z < zs + 16; z += 24) {
                    FakePlayer fp = new FakePlayer(Env.level, k++);
                    fp.snapTo(1000 + x, 110, 1000 + z, 0, 0);
                    fp.setGameMode(net.minecraft.world.level.GameType.SPECTATOR);
                    Env.level.addNewPlayer(fp);
                }
        });
        Env.waitTicks(20);
        Env.run(() -> {
            for (int x = 0; x <= xs; x += 32)
                for (int z = 0; z <= zs; z += 32)
                    Env.command(String.format("fill %d 99 %d %d 102 %d stone", 1000 + x, 1000 + z, 1000 + Math.min(x + 31, xs), 1000 + Math.min(z + 31, zs)));
        });
        Env.waitTicks(2);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        var lv = Env.level;
        Env.run(() -> {
            for (int i = 0; i < cells; i++) {
                Block b = blocks.get(i / 2);
                BlockPos p = cell(i, per);
                lv.setBlock(p, b.defaultBlockState(), 2 | 16);
            }
            for (int i = 0; i < cells; i++) {
                BlockPos p = cell(i, per).east();
                lv.setBlock(p, (i % 2 == 0 ? Blocks.WATER : Blocks.LAVA).defaultBlockState(), 3);
            }
        });
        String[] first = new String[cells];
        step(1);
        Env.run(() -> {
            for (int i = 0; i < cells; i++) first[i] = col(lv.getBlockState(cell(i, per)), blocks.get(i / 2), fluids[i % 2]);
        });
        step(44);
        Env.run(() -> {
            for (int i = 0; i < cells; i++) {
                String c = col(lv.getBlockState(cell(i, per)), blocks.get(i / 2), fluids[i % 2]);
                boolean tk = lv.shouldTickBlocksAt(net.minecraft.world.level.ChunkPos.pack(cell(i, per)));
                o.println(fluids[i % 2] + " " + BuiltInRegistries.BLOCK.getKey(blocks.get(i / 2)).getPath() + " " + first[i] + " " + c + (tk ? "" : " noticking"));
            }
        });
        Env.exit();
    }

    static void step(int n) throws Exception {
        for (int i = 0; i < n; i++) {
            long gt = Env.call(() -> Env.level.getGameTime());
            Env.run(() -> Env.server.tickRateManager().stepGameIfPaused(1));
            for (int k = 0; k < 4000 && Env.call(() -> Env.level.getGameTime()) == gt; k++) Thread.sleep(2);
        }
    }

    static BlockPos cell(int i, int per) {
        return new BlockPos(1001 + (i % per) * 4, 101, 1001 + (i / per) * 3);
    }

    static String col(BlockState s, Block b, String fluid) {
        if (s == b.defaultBlockState()) return "same";
        if (BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath().equals(fluid)) return "fluid";
        return Scenes.state(s);
    }
}
