package kremenmc.probe.feat_ore;

import java.io.PrintStream;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;

// Sturdy prints, per block, BlockState.isFaceSturdy (full support) for each
// of its states in definition order, as a 6-bit mask over Direction order
// (down, up, north, south, west, east). Blocks whose states all share one
// mask print it once.
public final class Sturdy {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        for (Block b : BuiltInRegistries.BLOCK) {
            var states = b.getStateDefinition().getPossibleStates();
            int[] m = new int[states.size()];
            boolean same = true;
            for (int i = 0; i < m.length; i++) {
                BlockState s = states.get(i);
                for (Direction d : Direction.values())
                    if (s.isFaceSturdy(EmptyBlockGetter.INSTANCE, BlockPos.ZERO, d)) m[i] |= 1 << d.ordinal();
                same &= m[i] == m[0];
            }
            StringBuilder sb = new StringBuilder(BuiltInRegistries.BLOCK.getKey(b).getPath()).append(' ').append(m.length);
            for (int i = 0; i < (same ? 1 : m.length); i++) sb.append(' ').append(m[i]);
            out.println(sb);
        }
        out.flush();
        Runtime.getRuntime().halt(0);
    }
}
