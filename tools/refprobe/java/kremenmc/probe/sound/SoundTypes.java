package kremenmc.probe.sound;

import java.util.*;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.SoundType;
import net.minecraft.world.level.block.state.BlockState;

// SoundTypes dumps every distinct block SoundType and which one each block
// (or, when it varies, each state) uses.
//
//	t <idx> <volume> <pitch> <break> <step> <place> <hit> <fall>
//	b <name> <idx>            every state uses idx
//	b <name> <idx>,<idx>,...  one idx per state, in state order
public final class SoundTypes {
    public static void main(String[] args) throws Exception {
        Env.init();
        var o = Env.out;
        Map<SoundType, Integer> ids = new IdentityHashMap<>();
        List<String> lines = new ArrayList<>();
        for (Block b : BuiltInRegistries.BLOCK) {
            List<Integer> per = new ArrayList<>();
            for (BlockState s : b.getStateDefinition().getPossibleStates()) {
                SoundType t = s.getSoundType();
                Integer id = ids.get(t);
                if (id == null) {
                    id = ids.size();
                    ids.put(t, id);
                    o.println("t " + id + " " + t.getVolume() + " " + t.getPitch() + " " + t.getBreakSound().location() + " " + t.getStepSound().location() + " "
                        + t.getPlaceSound().location() + " " + t.getHitSound().location() + " " + t.getFallSound().location());
                }
                per.add(id);
            }
            boolean same = per.stream().allMatch(x -> x.equals(per.get(0)));
            StringBuilder sb = new StringBuilder("b " + BuiltInRegistries.BLOCK.getKey(b).getPath() + " ");
            if (same) sb.append(per.get(0));
            else for (int i = 0; i < per.size(); i++) sb.append(i > 0 ? "," : "").append(per.get(i));
            lines.add(sb.toString());
        }
        for (String l : lines) o.println(l);
        o.flush();
        Runtime.getRuntime().halt(0);
    }
}
