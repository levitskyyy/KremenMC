package kremenmc.probe.bench;

import java.util.TreeMap;
import java.util.stream.Collectors;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.Heightmap;

/**
 * Writes, for every block state, vanilla's heightmap predicates
 * (Heightmap.Types.isOpaque) plus isAir and whether the fluid state is
 * non-empty, as a 6-character mask "WS OF MB MBNL air fluid" (1/0).
 * Blocks whose states all share a mask print one line "name\tmask";
 * otherwise one line per state "name[k=v,...]\tmask" (properties sorted).
 * The predicates depend on block tags, so this runs inside a started server
 * (StageOracle's "heightmaps" directive).
 */
final class HeightmapPredicates {
    static String dump() {
        Heightmap.Types[] types = {Heightmap.Types.WORLD_SURFACE, Heightmap.Types.OCEAN_FLOOR, Heightmap.Types.MOTION_BLOCKING, Heightmap.Types.MOTION_BLOCKING_NO_LEAVES};
        StringBuilder out = new StringBuilder();
        for (Block block : BuiltInRegistries.BLOCK) {
            String name = BuiltInRegistries.BLOCK.getKey(block).toString();
            TreeMap<String, String> masks = new TreeMap<>();
            for (BlockState s : block.getStateDefinition().getPossibleStates()) {
                StringBuilder m = new StringBuilder();
                for (Heightmap.Types t : types) m.append(t.isOpaque().test(s) ? '1' : '0');
                m.append(s.isAir() ? '1' : '0').append(s.getFluidState().isEmpty() ? '0' : '1');
                String props = s.getValues()
                    .map(v -> v.property().getName() + "=" + v.valueName())
                    .sorted()
                    .collect(Collectors.joining(","));
                masks.put(props.isEmpty() ? name : name + "[" + props + "]", m.toString());
            }
            if (masks.values().stream().distinct().count() == 1) {
                out.append(name).append('\t').append(masks.firstEntry().getValue()).append('\n');
            } else {
                masks.forEach((k, v) -> out.append(k).append('\t').append(v).append('\n'));
            }
        }
        return out.toString();
    }
}
