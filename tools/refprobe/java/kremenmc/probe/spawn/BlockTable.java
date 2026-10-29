package kremenmc.probe.spawn;

import java.lang.reflect.Proxy;
import java.lang.reflect.InvocationHandler;
import java.util.*;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.level.BlockGetter;
import net.minecraft.world.level.block.Block;

import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.material.FluidState;

// BlockTable prints, for every block state, the per-state properties natural
// spawning reads, evaluated against an isolated getter (state at origin, air
// elsewhere), plus each block's tags (tags need a booted server).
public final class BlockTable {
    static BlockGetter isolated(BlockState s, BlockPos at) {
        InvocationHandler h = (proxy, m, a) -> switch (m.getName()) {
            case "getBlockEntity" -> null;
            case "getBlockState" -> ((BlockPos) a[0]).equals(at) ? s : net.minecraft.world.level.block.Blocks.AIR.defaultBlockState();
            case "getFluidState" -> ((BlockPos) a[0]).equals(at) ? s.getFluidState() : net.minecraft.world.level.block.Blocks.AIR.defaultBlockState().getFluidState();
            case "getHeight" -> 384;
            case "getMinY" -> -64;
            case "hashCode" -> System.identityHashCode(proxy);
            case "equals" -> proxy == a[0];
            case "toString" -> "IsolatedGetter";
            default -> InvocationHandler.invokeDefault(proxy, m, a);
        };
        return (BlockGetter) Proxy.newProxyInstance(BlockGetter.class.getClassLoader(), new Class<?>[] {BlockGetter.class}, h);
    }

    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            BlockPos at = new BlockPos(0, 64, 0);
            List<EntityType<?>> types = new ArrayList<>();
            for (EntityType<?> t : BuiltInRegistries.ENTITY_TYPE) types.add(t);
            Map<String, Integer> classes = new LinkedHashMap<>();
            for (Block bl : BuiltInRegistries.BLOCK) {
                var ss = bl.getStateDefinition().getPossibleStates();
                List<String> pal = new ArrayList<>();
                StringBuilder idx = new StringBuilder();
                int prevIdx = -1, cnt = 0, first = Block.getId(ss.get(0));
                for (int k = 0; k < ss.size(); k++) {
                    BlockState s = ss.get(k);
                    if (Block.getId(s) != first + k) throw new IllegalStateException("non-contiguous states " + bl);
                    BlockGetter g = isolated(s, at);
                    BitSet bits = new BitSet();
                    for (int i = 0; i < types.size(); i++) if (s.isValidSpawn(g, at, types.get(i))) bits.set(i);
                    Integer cls = classes.computeIfAbsent(bits.toString(), x -> classes.size());
                    FluidState f = s.getFluidState();
                    String props = b(s.isRedstoneConductor(g, at)) + b(s.isCollisionShapeFullBlock(g, at)) + b(s.isSignalSource()) + b(s.isAir()) + b(s.liquid()) + b(s.isPathfindable(net.minecraft.world.level.pathfinder.PathComputationType.LAND))
                        + "/" + s.getLightEmission() + "/" + s.getLightDampening() + "/" + (f.isEmpty() ? "-" : BuiltInRegistries.FLUID.getKey(f.getType()).getPath() + ":" + f.getAmount() + ":" + b(f.isSource()))
                        + "/" + cls;
                    int pi = pal.indexOf(props);
                    if (pi < 0) {
                        pi = pal.size();
                        pal.add(props);
                    }
                    if (pi == prevIdx) {
                        cnt++;
                    } else {
                        if (cnt > 0) idx.append(' ').append(prevIdx).append('*').append(cnt);
                        prevIdx = pi;
                        cnt = 1;
                    }
                }
                idx.append(' ').append(prevIdx).append('*').append(cnt);
                o.println("b " + BuiltInRegistries.BLOCK.getKey(bl) + " " + first + " " + ss.size() + " " + Block.getId(bl.defaultBlockState()) + " " + String.join(" ", pal) + " |" + idx);
            }
            for (var en : classes.entrySet()) {
                StringBuilder sb = new StringBuilder("spawnclass " + en.getValue());
                String k = en.getKey();
                if (k.length() > 2) {
                    for (String idx : k.substring(1, k.length() - 1).split(", ")) sb.append(' ').append(EntityType.getKey(types.get(Integer.parseInt(idx))));
                }
                o.println(sb);
            }
        });
        Env.exit();
    }

    static String b(boolean v) {
        return v ? "1" : "0";
    }

    static String stateString(BlockState s) {
        StringBuilder sb = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString());
        var props = s.getProperties();
        if (!props.isEmpty()) {
            sb.append('[');
            boolean first = true;
            for (var p : props) {
                if (!first) sb.append(',');
                first = false;
                sb.append(p.getName()).append('=').append(name(p, s));
            }
            sb.append(']');
        }
        return sb.toString();
    }

    @SuppressWarnings({"unchecked", "rawtypes"})
    static String name(net.minecraft.world.level.block.state.properties.Property p, BlockState s) {
        return p.getName(s.getValue(p));
    }
}
