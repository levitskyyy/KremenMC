package kremenmc.probe.explosion;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.FireBlock;
import net.minecraft.world.level.block.state.BlockState;

// BlockData dumps the per-block and per-state properties explosions and fire
// placement read: explosion resistance (block and fluid), solid render, sturdy
// top face, flammability and the soul fire base tag.
//
//	b <name> <first state id> <states> <ignite> <burn> <soulBase 0/1>
//	s <id> <block res float bits> <fluid res float bits> <flags>
//
//
// flags: 1 solid render, 2 sturdy up face, 4 air, 8 non-empty fluid, 16 can burn.
public final class BlockData {
    public static void main(String[] args) throws Exception {
        Env.init();
        var o = Env.out;
        FireBlock fire = (FireBlock) Blocks.FIRE;
        Field ig = FireBlock.class.getDeclaredField("igniteOdds");
        Field bu = FireBlock.class.getDeclaredField("burnOdds");
        ig.setAccessible(true);
        bu.setAccessible(true);
        var igm = (it.unimi.dsi.fastutil.objects.Object2IntMap<?>) ig.get(fire);
        var bum = (it.unimi.dsi.fastutil.objects.Object2IntMap<?>) bu.get(fire);
        Method canBurn = FireBlock.class.getDeclaredMethod("canBurn", BlockState.class);
        canBurn.setAccessible(true);
        var g = EmptyBlockGetter.INSTANCE;
        // Tags are not bound without a running server; read the vanilla tag file.
        String soulTag = new String(BlockData.class.getResourceAsStream("/data/minecraft/tags/block/soul_fire_base_blocks.json").readAllBytes());
        for (Block b : BuiltInRegistries.BLOCK) {
            var states = b.getStateDefinition().getPossibleStates();
            int first = Block.getId(states.get(0));
            boolean soul = soulTag.contains("\"" + BuiltInRegistries.BLOCK.getKey(b) + "\"");
            o.println("b " + BuiltInRegistries.BLOCK.getKey(b).getPath() + " " + first + " " + states.size() + " "
                + igm.getInt(b) + " " + bum.getInt(b) + " " + (soul ? 1 : 0));
            for (BlockState s : states) {
                int f = 0;
                if (s.isSolidRender()) f |= 1;
                if (s.isFaceSturdy(g, BlockPos.ZERO, Direction.UP)) f |= 2;
                if (s.isAir()) f |= 4;
                if (!s.getFluidState().isEmpty()) f |= 8;
                if ((boolean) canBurn.invoke(fire, s)) f |= 16;
                o.println("s " + Block.getId(s) + " " + Integer.toHexString(Float.floatToRawIntBits(b.getExplosionResistance())) + " "
                    + Integer.toHexString(Float.floatToRawIntBits(s.getFluidState().getExplosionResistance())) + " " + f);
            }
        }
        o.flush();
        Runtime.getRuntime().halt(0);
    }
}
